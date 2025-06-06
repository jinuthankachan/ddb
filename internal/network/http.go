package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jinuthankachan/ddb/internal/kvstore"
	"github.com/jinuthankachan/ddb/internal/pbft"
)

// NetworkManager handles HTTP communication between nodes
type NetworkManager struct {
	nodeID     string
	httpClient *http.Client
	httpServer *http.Server
	peerAddrs  map[string]string // Map node ID to address
	listenAddr string
	handleFunc func(senderID string, msgType string, msgBytes []byte) error
	kvStore    *kvstore.SafeKVStore // Reference to the key-value store
	mu         sync.RWMutex // For thread-safe peer access
}

// NewNetworkManager creates a new network manager
func NewNetworkManager(nodeID, listenAddr string, peerAddrs map[string]string,
	msgHandler func(senderID string, msgType string, msgBytes []byte) error,
	kvStore *kvstore.SafeKVStore) *NetworkManager {

	return &NetworkManager{
		nodeID:     nodeID,
		listenAddr: listenAddr,
		peerAddrs:  peerAddrs,
		handleFunc: msgHandler,
		kvStore:    kvStore,
		httpClient: &http.Client{
			Timeout: 10 * time.Second, // Increased timeout
		},
	}
}

// Start starts the HTTP server
func (nm *NetworkManager) Start() error {
	mux := http.NewServeMux()

	// Handler for PBFT messages
	mux.HandleFunc("/pbft/message", nm.handlePBFTMessage)

	// Handler for client requests (if this node exposes client endpoints)
	mux.HandleFunc("/kv/", nm.handleClientRequest)

	nm.httpServer = &http.Server{
		Addr:         nm.listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	fmt.Printf("Node %s starting HTTP server on %s\n", nm.nodeID, nm.listenAddr)

	// Start the server in a goroutine
	go func() {
		if err := nm.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server
func (nm *NetworkManager) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return nm.httpServer.Shutdown(ctx)
}

// handlePBFTMessage processes incoming PBFT protocol messages
func (nm *NetworkManager) handlePBFTMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()

	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	// Extract sender ID and message type from the JSON
	var baseMsg struct {
		Type     string `json:"type"`
		SenderID string `json:"sender_id"`
	}

	if err := json.Unmarshal(body, &baseMsg); err != nil {
		http.Error(w, "Invalid message format", http.StatusBadRequest)
		return
	}

	// Special handling for REPLY messages
	if baseMsg.Type == string(pbft.TypeReply) {
		// REPLY messages are sent to clients, not processed by the PBFT engine
		// Just acknowledge receipt without forwarding to the PBFT engine
		fmt.Printf("Node %s received REPLY message from %s, acknowledging without processing\n", 
			nm.nodeID, baseMsg.SenderID)
	} else {
		// Pass all other message types to the PBFT engine
		if err := nm.handleFunc(baseMsg.SenderID, baseMsg.Type, body); err != nil {
			http.Error(w, fmt.Sprintf("Error processing message: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Acknowledge receipt with 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}


// BroadcastMessage sends a PBFT message to all peers
func (nm *NetworkManager) BroadcastMessage(message interface{}) error {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("error marshaling message: %w", err)
	}

	nm.mu.RLock()
	peers := make(map[string]string, len(nm.peerAddrs))
	for id, addr := range nm.peerAddrs {
		peers[id] = addr
	}
	nm.mu.RUnlock()

	var wg sync.WaitGroup
	errorCh := make(chan error, len(peers))

	for peerID, peerAddr := range peers {
		if peerID == nm.nodeID {
			// Skip self (could process locally instead)
			continue
		}

		wg.Add(1)
		go func(id, addr string) {
			defer wg.Done()

			url := fmt.Sprintf("http://%s/pbft/message", addr)
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
			if err != nil {
				errorCh <- fmt.Errorf("error creating request to %s: %w", id, err)
				return
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := nm.httpClient.Do(req)
			if err != nil {
				errorCh <- fmt.Errorf("error sending to %s: %w", id, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusAccepted {
				body, _ := io.ReadAll(resp.Body)
				errorCh <- fmt.Errorf("unexpected status from %s: %d - %s", id, resp.StatusCode, string(body))
				return
			}
		}(peerID, peerAddr)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorCh)

	// Collect any errors
	var errors []error
	for err := range errorCh {
		errors = append(errors, err)
	}

	// PBFT only needs f+1 successful deliveries, so some failures are acceptable
	// For simplicity, we'll just log the errors for now
	for _, err := range errors {
		fmt.Printf("Broadcast error: %v\n", err)
	}

	return nil
}

// SendMessageToPeer sends a PBFT message to a specific peer
func (nm *NetworkManager) SendMessageToPeer(peerID string, message interface{}) error {
	nm.mu.RLock()
	peerAddr, exists := nm.peerAddrs[peerID]
	nm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("unknown peer ID: %s", peerID)
	}

	jsonData, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("error marshaling message: %w", err)
	}

	url := fmt.Sprintf("http://%s/pbft/message", peerAddr)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := nm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status: %d - %s", resp.StatusCode, string(body))
	}

	return nil
}

// UpdatePeers updates the peer address map
func (nm *NetworkManager) UpdatePeers(peerAddrs map[string]string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Copy the new peer addresses
	nm.peerAddrs = make(map[string]string, len(peerAddrs))
	for id, addr := range peerAddrs {
		nm.peerAddrs[id] = addr
	}
}

// handleClientRequest processes incoming client requests (e.g., SET, GET, DELETE)
func (nm *NetworkManager) handleClientRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract key from URL path
	key := strings.TrimPrefix(r.URL.Path, "/kv/")
	if key == "" && r.Method != http.MethodGet {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	var op pbft.Operation
	clientID := "external-client" // In a real system, we'd authenticate clients

	switch r.Method {
	case http.MethodGet:
		if key == "" {
			// List all keys - this would require a new operation type
			http.Error(w, "Listing all keys not implemented", http.StatusNotImplemented)
			return
		}
		op = pbft.Operation{
			Type: "GET",
			Key:  key,
		}

	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}

		// Try to parse the body as JSON with a "value" field
		var jsonData struct {
			Value string `json:"value"`
		}
		
		if err := json.Unmarshal(body, &jsonData); err == nil && jsonData.Value != "" {
			// Successfully parsed JSON with a value field
			op = pbft.Operation{
				Type:  "SET",
				Key:   key,
				Value: []byte(jsonData.Value),
			}
		} else {
			// Fall back to using the raw body as the value
			op = pbft.Operation{
				Type:  "SET",
				Key:   key,
				Value: body,
			}
		}

	case http.MethodDelete:
		op = pbft.Operation{
			Type: "DELETE",
			Key:  key,
		}
	}

	// Create a request message
	request := pbft.NewRequestMessage(clientID, op)
	
	// Marshal the request to JSON bytes
	requestBytes, err := json.Marshal(request)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error marshaling request: %v", err), http.StatusInternalServerError)
		return
	}

	// For GET operations, we can read directly from the local store for simplicity
	// In a production system, you might want consensus on reads too
	if op.Type == "GET" {
		if nm.kvStore == nil {
			http.Error(w, "KV store not initialized", http.StatusInternalServerError)
			return
		}

		value, exists := nm.kvStore.Get(key)
		
		w.Header().Set("Content-Type", "application/json")
		
		if !exists {
			// Key not found
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"error":  "Key not found",
			})
			return
		}
		
		// Key found, return the value
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"key":    key,
			"value":  string(value),
		})
		return
	}

	// Pass to the message handler (in a real system, we'd wait for the reply)
	err = nm.handleFunc(clientID, string(pbft.TypeRequest), requestBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error processing request: %v", err), http.StatusInternalServerError)
		return
	}

	// In a real system, we'd wait for consensus before responding
	// For now, just acknowledge receipt
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "accepted",
		"message": "Request accepted for processing",
		"key":     key,
		"op":      op.Type,
	})
}

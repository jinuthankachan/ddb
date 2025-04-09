package network

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// NetworkManager handles HTTP communication between nodes
type NetworkManager struct {
	nodeID     string
	httpClient *http.Client
	httpServer *http.Server
	peerAddrs  map[string]string // Map node ID to address
	listenAddr string
	handleFunc func(senderID string, msgType string, msgBytes []byte) error
	mu         sync.RWMutex // For thread-safe peer access
}

// NewNetworkManager creates a new network manager
func NewNetworkManager(nodeID, listenAddr string, peerAddrs map[string]string,
	msgHandler func(senderID string, msgType string, msgBytes []byte) error) *NetworkManager {

	return &NetworkManager{
		nodeID:     nodeID,
		listenAddr: listenAddr,
		peerAddrs:  peerAddrs,
		handleFunc: msgHandler,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // Reasonable default timeout
		},
	}
}

// Start starts the HTTP server
func (nm *NetworkManager) Start() error {
	mux := http.NewServeMux()

	// Handler for PBFT messages
	mux.HandleFunc("/pbft/message", nm.handlePBFTMessage)

	// Handler for client requests (if this node exposes client endpoints)
	mux.HandleFunc("/kv", nm.handleClientRequest)

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

// handlePBFTMessage processes incoming PBFT messages
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

	// Pass the message to the PBFT engine
	if err := nm.handleFunc(baseMsg.SenderID, baseMsg.Type, body); err != nil {
		http.Error(w, fmt.Sprintf("Error processing message: %v", err), http.StatusInternalServerError)
		return
	}

	// Acknowledge receipt with 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

// handleClientRequest processes incoming client requests (e.g., SET, GET, DELETE)
func (nm *NetworkManager) handleClientRequest(w http.ResponseWriter, r *http.Request) {
	// This would be expanded in a real implementation
	// For now, just return a not implemented error
	http.Error(w, "Client API not implemented yet", http.StatusNotImplemented)
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

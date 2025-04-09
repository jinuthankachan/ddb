package pbft

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jinuthankachan/ddb/internal/kvstore"
)

// Interface for the network layer
type NetworkManager interface {
	BroadcastMessage(message interface{}) error
	SendMessageToPeer(peerID string, message interface{}) error
}

// Node represents a PBFT node in the network
type Node struct {
	ID             string
	kvStore        *kvstore.SafeKVStore
	networkManager NetworkManager
	state          *PBFTState
	isPrimary      bool
	totalNodes     int
	faultTolerance int // Maximum number of faulty nodes (f)

	// Channels for asynchronous processing
	requestCh     chan *RequestMessage
	prepreparedCh chan *PrePrepareMessage
	preparedCh    chan *PrepareMessage
	committedCh   chan *CommitMessage

	// Shutdown signaling
	stopCh   chan struct{}
	stopped  bool
	stopOnce sync.Once

	// Additional synchronization
	mu sync.RWMutex
}

// NewNode creates a new PBFT node
func NewNode(id string, kvStore *kvstore.SafeKVStore, networkManager NetworkManager,
	totalNodes, faultTolerance int) *Node {

	node := &Node{
		ID:             id,
		kvStore:        kvStore,
		networkManager: networkManager,
		state:          NewPBFTState(faultTolerance),
		totalNodes:     totalNodes,
		faultTolerance: faultTolerance,
		requestCh:      make(chan *RequestMessage, 100),
		prepreparedCh:  make(chan *PrePrepareMessage, 100),
		preparedCh:     make(chan *PrepareMessage, 100),
		committedCh:    make(chan *CommitMessage, 100),
		stopCh:         make(chan struct{}),
		stopped:        false,
	}

	// Determine if this node is the initial primary (view 0)
	node.isPrimary = (id == node.getPrimaryForView(0))

	return node
}

// Start initializes and starts the PBFT node
func (n *Node) Start() {
	fmt.Printf("Starting PBFT node %s (primary: %v)\n", n.ID, n.isPrimary)

	// Start processing goroutines
	go n.processClientRequests()
	go n.processPrePrepareMessages()
	go n.processPrepareMessages()
	go n.processCommitMessages()
}

// Stop gracefully shuts down the PBFT node
func (n *Node) Stop() {
	n.stopOnce.Do(func() {
		fmt.Printf("Stopping PBFT node %s\n", n.ID)
		
		// Signal all goroutines to stop
		close(n.stopCh)
		
		n.mu.Lock()
		n.stopped = true
		n.mu.Unlock()
		
		// Note: We intentionally don't close the message channels here
		// as they might still be receiving messages from the network layer.
		// The goroutines will exit when they detect the stop signal.
	})
}
// HandleMessage processes an incoming PBFT message
func (n *Node) HandleMessage(senderID, msgType string, msgBytes []byte) error {
	switch MessageType(msgType) {
	case TypeRequest:
		var msg RequestMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return fmt.Errorf("error unmarshaling REQUEST message: %w", err)
		}
		n.requestCh <- &msg

	case TypePrePrepare:
		var msg PrePrepareMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return fmt.Errorf("error unmarshaling PRE-PREPARE message: %w", err)
		}
		n.prepreparedCh <- &msg

	case TypePrepare:
		var msg PrepareMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return fmt.Errorf("error unmarshaling PREPARE message: %w", err)
		}
		n.preparedCh <- &msg

	case TypeCommit:
		var msg CommitMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return fmt.Errorf("error unmarshaling COMMIT message: %w", err)
		}
		n.committedCh <- &msg

	// We'll add handlers for VIEW-CHANGE, NEW-VIEW, and CHECKPOINT later

	default:
		return fmt.Errorf("unknown message type: %s", msgType)
	}

	return nil
}

// processClientRequests handles incoming client requests (primary only)
func (n *Node) processClientRequests() {
	for {
		select {
		case <-n.stopCh:
			return // Exit when stop signal received
		case req, ok := <-n.requestCh:
			if !ok {
				return // Channel closed
			}
			if !n.isPrimary {
				fmt.Printf("Node %s (not primary) received client request, forwarding to primary\n", n.ID)
				primaryID := n.getPrimaryForView(n.state.View)
				n.networkManager.SendMessageToPeer(primaryID, req)
				continue
			}

			// Assign a sequence number and start the consensus process
			n.mu.Lock()
			seq := n.state.NextSequence
			n.state.NextSequence++
			n.mu.Unlock()

			// Create and send PRE-PREPARE message
			prePrepare := NewPrePrepareMessage(n.state.View, seq, n.ID, req)

			fmt.Printf("Node %s (primary) broadcasting PRE-PREPARE for seq %d\n", n.ID, seq)
			n.state.LogPrePrepare(prePrepare)
			n.networkManager.BroadcastMessage(prePrepare)
		}
	}
}

// processPrePrepareMessages handles PRE-PREPARE messages (backups)
func (n *Node) processPrePrepareMessages() {
	for {
		select {
		case <-n.stopCh:
			return // Exit when stop signal received
		case msg, ok := <-n.prepreparedCh:
			if !ok {
				return // Channel closed
			}
			// Verify the message is from the current primary
			primaryID := n.getPrimaryForView(msg.View)
			if msg.SenderID != primaryID {
				fmt.Printf("Node %s received PRE-PREPARE from non-primary %s, ignoring\n",
					n.ID, msg.SenderID)
				continue
			}

			// Verify the view matches our current view
			if msg.View != n.state.View {
				fmt.Printf("Node %s received PRE-PREPARE for view %d, but current view is %d\n",
					n.ID, msg.View, n.state.View)
				continue
			}

			// Verify we haven't already processed this sequence number
			if n.state.HasPrePrepared(msg.View, msg.SequenceNumber) {
				fmt.Printf("Node %s already processed PRE-PREPARE for view %d, seq %d\n",
					n.ID, msg.View, msg.SequenceNumber)
				continue
			}

			// Verify the request digest matches the request
			if msg.Request != nil {
				computedDigest := ComputeDigest(msg.Request)
				if computedDigest != msg.RequestDigest {
					fmt.Printf("Node %s detected digest mismatch for PRE-PREPARE view %d, seq %d\n",
						n.ID, msg.View, msg.SequenceNumber)
					continue
				}
			}

			// Log the PRE-PREPARE message
			n.state.LogPrePrepare(msg)

			// Send PREPARE message
			prepare := NewPrepareMessage(msg.View, msg.SequenceNumber, n.ID, msg.RequestDigest)
			fmt.Printf("Node %s sending PREPARE for view %d, seq %d\n",
				n.ID, msg.View, msg.SequenceNumber)
			n.state.LogPrepare(prepare)
			n.networkManager.BroadcastMessage(prepare)
		}
	}
}
// processPrepareMessages handles PREPARE messages
func (n *Node) processPrepareMessages() {
	for {
		select {
		case <-n.stopCh:
			return // Exit when stop signal received
		case msg, ok := <-n.preparedCh:
			if !ok {
				return // Channel closed
			}
			// Verify the view matches our current view
			if msg.View != n.state.View {
				fmt.Printf("Node %s received PREPARE for view %d, but current view is %d\n",
					n.ID, msg.View, n.state.View)
				continue
			}

			// Log the PREPARE message
			n.state.LogPrepare(msg)

			// Check if we have enough PREPARE messages to enter the prepared state
			if n.state.HasPrepared(msg.View, msg.SequenceNumber, msg.RequestDigest) {
				// Send COMMIT message
				commit := NewCommitMessage(msg.View, msg.SequenceNumber, n.ID, msg.RequestDigest)
				fmt.Printf("Node %s sending COMMIT for view %d, seq %d\n",
					n.ID, msg.View, msg.SequenceNumber)
				n.state.LogCommit(commit)
				n.networkManager.BroadcastMessage(commit)
			}
		}
	}
}

// processCommitMessages handles COMMIT messages
func (n *Node) processCommitMessages() {
	for {
		select {
		case <-n.stopCh:
			return // Exit when stop signal received
		case msg, ok := <-n.committedCh:
			if !ok {
				return // Channel closed
			}
			// Verify the view matches our current view
			if msg.View != n.state.View {
				fmt.Printf("Node %s received COMMIT for view %d, but current view is %d\n",
					n.ID, msg.View, n.state.View)
				continue
			}

			// Log the COMMIT message
			n.state.LogCommit(msg)

			// Check if we have enough COMMIT messages to execute the operation
			if n.state.HasCommitted(msg.View, msg.SequenceNumber, msg.RequestDigest) {
				// Get the original request from the state
				request := n.state.GetRequest(msg.View, msg.SequenceNumber)
				if request == nil {
					fmt.Printf("Node %s: Cannot find original request for committed operation v:%d s:%d\n",
						n.ID, msg.View, msg.SequenceNumber)
					continue
				}

				// Execute the operation
				result, status := n.executeOperation(request.Operation)

				fmt.Printf("Node %s executed operation for seq %d: %s %s\n",
					n.ID, msg.SequenceNumber, request.Operation.Type, request.Operation.Key)

				// Create and send reply to the client
				reply := NewReplyMessage(msg.View, msg.SequenceNumber, n.ID, request.ClientID, result, status)
				
				// In a real implementation, we would send this directly to the client
				// For now, we can broadcast it to simulate all nodes replying
				n.networkManager.BroadcastMessage(reply)
			}
		}
	}
}

// executeOperation applies the operation to the KV store
func (n *Node) executeOperation(op Operation) ([]byte, string) {
	switch op.Type {
	case "SET":
		n.kvStore.Set(op.Key, op.Value)
		return nil, "SUCCESS"

	case "GET":
		value, exists := n.kvStore.Get(op.Key)
		if !exists {
			return nil, "KEY_NOT_FOUND"
		}
		return value, "SUCCESS"

	case "DELETE":
		n.kvStore.Delete(op.Key)
		return nil, "SUCCESS"

	default:
		return nil, "INVALID_OPERATION"
	}
}

// getPrimaryForView determines the primary node ID for a given view
func (n *Node) getPrimaryForView(view uint64) string {
	// Simple round-robin approach - you would replace this with actual node IDs
	return fmt.Sprintf("node%d", view%uint64(n.totalNodes))
}

// IsPrimary returns true if this node is the current primary
func (n *Node) IsPrimary() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isPrimary
}

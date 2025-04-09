package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/your-username/distributed-kv-store/internal/kvstore"
	"github.com/your-username/distributed-kv-store/internal/network"
	"github.com/your-username/distributed-kv-store/internal/pbft"
)

func main() {
	// Parse command line arguments
	nodeID := flag.String("id", "", "Node ID (required)")
	listenAddr := flag.String("addr", ":8080", "HTTP listen address")
	peersStr := flag.String("peers", "", "Comma-separated list of peers in format id=host:port")
	faultTolerance := flag.Int("f", 1, "Fault tolerance (f)")
	flag.Parse()

	if *nodeID == "" {
		fmt.Println("Node ID is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse peers
	peers := make(map[string]string)
	if *peersStr != "" {
		peersList := strings.Split(*peersStr, ",")
		for _, peer := range peersList {
			parts := strings.Split(peer, "=")
			if len(parts) != 2 {
				fmt.Printf("Invalid peer format: %s\n", peer)
				continue
			}
			peers[parts[0]] = parts[1]
		}
	}

	// Create KV Store
	kvStore := kvstore.NewSafeKVStore()

	// Create a channel for message handling
	msgHandlerCh := make(chan func(), 100)

	// Create network manager
	networkManager := network.NewNetworkManager(
		*nodeID,
		*listenAddr,
		peers,
		func(senderID, msgType string, msgBytes []byte) error {
			// Forward to the PBFT node for processing
			return node.HandleMessage(senderID, msgType, msgBytes)
		},
	)

	// Calculate total nodes
	totalNodes := len(peers) + 1 // +1 for self

	// Create PBFT node
	node := pbft.NewNode(
		*nodeID,
		kvStore,
		networkManager,
		totalNodes,
		*faultTolerance,
	)

	// Start network manager
	if err := networkManager.Start(); err != nil {
		fmt.Printf("Failed to start network manager: %v\n", err)
		os.Exit(1)
	}

	// Start PBFT node
	node.Start()

	fmt.Printf("Node %s started. Press Ctrl+C to exit.\n", *nodeID)

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down...")
	networkManager.Stop()
}

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	// Parse command line arguments
	nodeAddr := flag.String("node", "localhost:8080", "Node address")
	operation := flag.String("op", "", "Operation: get, set, delete")
	key := flag.String("key", "", "Key")
	value := flag.String("value", "", "Value (for set operation)")
	flag.Parse()

	if *operation == "" || *key == "" {
		fmt.Println("Operation and key are required")
		flag.Usage()
		os.Exit(1)
	}

	var method string
	var url string
	var body io.Reader

	switch strings.ToLower(*operation) {
	case "get":
		method = http.MethodGet
		url = fmt.Sprintf("http://%s/kv/%s", *nodeAddr, *key)

	case "set":
		if *value == "" {
			fmt.Println("Value is required for set operation")
			flag.Usage()
			os.Exit(1)
		}
		method = http.MethodPost
		url = fmt.Sprintf("http://%s/kv/%s", *nodeAddr, *key)
		body = bytes.NewBufferString(*value)

	case "delete":
		method = http.MethodDelete
		url = fmt.Sprintf("http://%s/kv/%s", *nodeAddr, *key)

	default:
		fmt.Printf("Unknown operation: %s\n", *operation)
		flag.Usage()
		os.Exit(1)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Create request
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1)
	}

	// Print status and response
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Response: %s\n", string(respBody))
}

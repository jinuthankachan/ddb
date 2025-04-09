package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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
	debug := flag.Bool("debug", false, "Enable debug logging")
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
		// Using a JSON structure for the value
		jsonValue, err := json.Marshal(map[string]string{"value": *value})
		if err != nil {
			fmt.Printf("Error marshaling JSON: %v\n", err)
			os.Exit(1)
		}
		body = bytes.NewBuffer(jsonValue)

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
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	// Add appropriate headers
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	if *debug {
		log.Printf("DEBUG: Sending %s request to %s", method, url)
		if body != nil {
			log.Printf("DEBUG: Request body: %v", body)
		}
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

	if *debug {
		log.Printf("DEBUG: Received status code: %d", resp.StatusCode)
		log.Printf("DEBUG: Response headers: %v", resp.Header)
		log.Printf("DEBUG: Response body: %s", string(respBody))
	}

	// Check status code
	if resp.StatusCode >= 400 {
		fmt.Printf("Error: %s\n", resp.Status)
		fmt.Printf("Response: %s\n", string(respBody))
		os.Exit(1)
	}

	// Print status and response
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Response: %s\n", string(respBody))
}

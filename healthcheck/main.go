package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type HealthResponse struct {
	Status string `json:"status"`
}

func main() {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}

	url := fmt.Sprintf("http://localhost:%s/api/v1/health", portStr)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Printf("request failed: %v", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("unexpected status code: %d", resp.StatusCode)
		os.Exit(1)
	}

	var result HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("invalid JSON response: %v", err)
		os.Exit(1)
	}

	if result.Status != "healthy" {
		fmt.Printf("unexpected health status: %s", result.Status)
		os.Exit(1)
	}

	fmt.Println("Health check passed.")
}

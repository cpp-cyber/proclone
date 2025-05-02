package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ProxmoxConfig holds the configuration for Proxmox API
type ProxmoxConfig struct {
	Host      string
	Port      string
	APIToken  string // API token for authentication
	VerifySSL bool
	Nodes     []string
}

// ProxmoxAPIResponse represents the generic Proxmox API response structure
type ProxmoxAPIResponse struct {
	Data json.RawMessage `json:"data"`
}

// ProxmoxNodeStatus represents the status response from a Proxmox node
type ProxmoxNodeStatus struct {
	CPU    float64 `json:"cpu"`
	Memory struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"memory"`
	Storage struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"storage"`
}

// loadProxmoxConfig loads and validates Proxmox configuration from environment variables
func loadProxmoxConfig() (*ProxmoxConfig, error) {
	tokenID := os.Getenv("PROXMOX_TOKEN_ID")         // The token ID including user and realm
	tokenSecret := os.Getenv("PROXMOX_TOKEN_SECRET") // The secret part of the token

	if tokenID == "" {
		return nil, fmt.Errorf("PROXMOX_TOKEN_ID is required")
	}
	if tokenSecret == "" {
		return nil, fmt.Errorf("PROXMOX_TOKEN_SECRET is required")
	}

	config := &ProxmoxConfig{
		Host:      os.Getenv("PROXMOX_SERVER"),
		Port:      os.Getenv("PROXMOX_PORT"),
		APIToken:  fmt.Sprintf("%s=%s", tokenID, tokenSecret),
		VerifySSL: os.Getenv("PROXMOX_VERIFY_SSL") == "true",
	}

	// Validate required fields
	if config.Host == "" {
		return nil, fmt.Errorf("PROXMOX_SERVER is required")
	}
	if config.Port == "" {
		config.Port = "443" // Default port
	}

	// Parse nodes list
	nodesStr := os.Getenv("PROXMOX_NODES")
	if nodesStr != "" {
		config.Nodes = strings.Split(nodesStr, ",")
	}

	return config, nil
}

// getNodeStatus fetches the status of a single Proxmox node
func getNodeStatus(config *ProxmoxConfig, nodeName string) (*ProxmoxNodeStatus, error) {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare status URL
	statusURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/status", config.Host, config.Port, nodeName)

	// Create request
	req, err := http.NewRequest("GET", statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add Authorization header with API token
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get node status: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read status response: %v", err)
	}

	// Parse response
	var apiResp ProxmoxAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %v", err)
	}

	// Extract status from response
	var status ProxmoxNodeStatus
	if err := json.Unmarshal(apiResp.Data, &status); err != nil {
		return nil, fmt.Errorf("failed to extract status from response: %v", err)
	}

	return &status, nil
}

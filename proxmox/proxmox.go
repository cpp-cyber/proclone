package proxmox

import (
	"encoding/json"
	"fmt"
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
}

// LoadProxmoxConfig loads and validates Proxmox configuration from environment variables
func LoadProxmoxConfig() (*ProxmoxConfig, error) {
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

// GetNodeStatus fetches the status of a single Proxmox node
func GetNodeStatus(config *ProxmoxConfig, nodeName string) (*ProxmoxNodeStatus, error) {

	// Prepare status endpoint path
	path := fmt.Sprintf("api2/json/nodes/%s/status", nodeName)

	_, body, err := MakeRequest(config, path, "GET", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("proxmox node status request failed: %v", err)
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

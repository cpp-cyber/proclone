package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ProxmoxConfig holds the configuration for Proxmox API
type ProxmoxConfig struct {
	Host      string
	Port      string
	APIToken  string    // API token for authentication
	VerifySSL bool
	Nodes     []string
}

// NodeResourceUsage represents the resource usage metrics for a single node
type NodeResourceUsage struct {
	NodeName     string  `json:"node_name"`
	CPUUsage     float64 `json:"cpu_usage"`      // CPU usage percentage
	MemoryTotal  int64   `json:"memory_total"`   // Total memory in bytes
	MemoryUsed   int64   `json:"memory_used"`    // Used memory in bytes
	StorageTotal int64   `json:"storage_total"`  // Total storage in bytes
	StorageUsed  int64   `json:"storage_used"`   // Used storage in bytes
}

// ResourceUsageResponse represents the API response containing resource usage for all nodes
type ResourceUsageResponse struct {
	Nodes   []NodeResourceUsage `json:"nodes"`
	Cluster struct {
		TotalCPUUsage    float64 `json:"total_cpu_usage"`     // Average CPU usage across all nodes
		TotalMemoryTotal int64   `json:"total_memory_total"`  // Total memory across all nodes
		TotalMemoryUsed  int64   `json:"total_memory_used"`   // Total used memory across all nodes
		TotalStorageTotal int64  `json:"total_storage_total"` // Total storage across all nodes
		TotalStorageUsed  int64  `json:"total_storage_used"`  // Total used storage across all nodes
	} `json:"cluster"`
	Errors  []string            `json:"errors,omitempty"`
}

// ProxmoxAPIResponse represents the generic Proxmox API response structure
type ProxmoxAPIResponse struct {
	Data json.RawMessage `json:"data"`
}

// ProxmoxNodeStatus represents the status response from a Proxmox node
type ProxmoxNodeStatus struct {
	CPU     float64 `json:"cpu"`
	Memory  struct {
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
	tokenID := os.Getenv("PROXMOX_TOKEN_ID")      // The token ID including user and realm
	tokenSecret := os.Getenv("PROXMOX_TOKEN_SECRET")  // The secret part of the token

	if tokenID == "" {
		return nil, fmt.Errorf("PROXMOX_TOKEN_ID is required")
	}
	if tokenSecret == "" {
		return nil, fmt.Errorf("PROXMOX_TOKEN_SECRET is required")
	}

	config := &ProxmoxConfig{
		Host:      os.Getenv("PROXMOX_HOST"),
		Port:      os.Getenv("PROXMOX_PORT"),
		APIToken:  fmt.Sprintf("%s=%s", tokenID, tokenSecret),
		VerifySSL: os.Getenv("PROXMOX_VERIFY_SSL") == "true",
	}

	// Validate required fields
	if config.Host == "" {
		return nil, fmt.Errorf("PROXMOX_HOST is required")
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

// getProxmoxResources handles the GET request to fetch resource usage from all Proxmox nodes
func getProxmoxResources(c *gin.Context) {
	// Get session
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Double check admin status (although middleware should have caught this)
	if !isAdmin.(bool) {
		log.Printf("Unauthorized access attempt by user %s", username)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only admin users can access resource usage data",
		})
		return
	}

	// Load Proxmox configuration
	config, err := loadProxmoxConfig()
	if err != nil {
		log.Printf("Configuration error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load Proxmox configuration: %v", err),
		})
		return
	}

	// If no nodes specified, return empty response
	if len(config.Nodes) == 0 {
		log.Printf("No nodes configured for user %s", username)
		c.JSON(http.StatusOK, ResourceUsageResponse{Nodes: []NodeResourceUsage{}})
		return
	}

	// Fetch status for each node
	var nodes []NodeResourceUsage
	var errors []string
	response := ResourceUsageResponse{}

	for _, nodeName := range config.Nodes {
		status, err := getNodeStatus(config, nodeName)
		if err != nil {
			errorMsg := fmt.Sprintf("Error fetching status for node %s: %v", nodeName, err)
			log.Printf(errorMsg)
			errors = append(errors, errorMsg)
			continue
		}

		nodes = append(nodes, NodeResourceUsage{
			NodeName:     nodeName,
			CPUUsage:     status.CPU,
			MemoryTotal:  status.Memory.Total,
			MemoryUsed:   status.Memory.Used,
			StorageTotal: status.Storage.Total,
			StorageUsed:  status.Storage.Used,
		})

		// Add to cluster totals
		response.Cluster.TotalMemoryTotal += status.Memory.Total
		response.Cluster.TotalMemoryUsed += status.Memory.Used
		response.Cluster.TotalStorageTotal += status.Storage.Total
		response.Cluster.TotalStorageUsed += status.Storage.Used
		response.Cluster.TotalCPUUsage += status.CPU
	}

	// Calculate average CPU usage for the cluster
	if len(nodes) > 0 {
		response.Cluster.TotalCPUUsage /= float64(len(nodes))
	}

	response.Nodes = nodes
	response.Errors = errors

	// If we have any errors but also some successful responses, include errors in response
	if len(errors) > 0 && len(nodes) > 0 {
		c.JSON(http.StatusPartialContent, response)
		return
	}

	// If we have only errors, return error status
	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch resource usage for any nodes",
			"details": errors,
		})
		return
	}

	// Success case
	log.Printf("Successfully fetched resource usage for user %s", username)
	c.JSON(http.StatusOK, response)
} 
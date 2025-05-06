package proxmox

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// NodeResourceUsage represents the resource usage metrics for a single node
type NodeResourceUsage struct {
	NodeName     string  `json:"node_name"`
	CPUUsage     float64 `json:"cpu_usage"`     // CPU usage percentage
	MemoryTotal  int64   `json:"memory_total"`  // Total memory in bytes
	MemoryUsed   int64   `json:"memory_used"`   // Used memory in bytes
	StorageTotal int64   `json:"storage_total"` // Total storage in bytes
	StorageUsed  int64   `json:"storage_used"`  // Used storage in bytes
}

// ResourceUsageResponse represents the API response containing resource usage for all nodes
type ResourceUsageResponse struct {
	Nodes   []NodeResourceUsage `json:"nodes"`
	Cluster struct {
		TotalCPUUsage     float64 `json:"total_cpu_usage"`     // Average CPU usage across all nodes
		TotalMemoryTotal  int64   `json:"total_memory_total"`  // Total memory across all nodes
		TotalMemoryUsed   int64   `json:"total_memory_used"`   // Total used memory across all nodes
		TotalStorageTotal int64   `json:"total_storage_total"` // Total storage across all nodes
		TotalStorageUsed  int64   `json:"total_storage_used"`  // Total used storage across all nodes
	} `json:"cluster"`
	Errors []string `json:"errors,omitempty"`
}

func GetProxmoxResources(c *gin.Context) {
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
	config, err := LoadProxmoxConfig()
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

	VirtualResources, err := GetVirtualResources(config)

	if err != nil {
		log.Printf("Failed to get proxmox cluster resources: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to get proxmox cluster resources: %v", err),
		})
		return
	}

	for _, nodeName := range config.Nodes {
		status, err := getNodeStatus(config, nodeName)
		if err != nil {
			errorMsg := fmt.Sprintf("Error fetching status for node %s: %v", nodeName, err)
			log.Printf("%s", errorMsg)
			errors = append(errors, errorMsg)
			continue
		}

		usedStorage, totalStorage := getNodeStorage(VirtualResources, nodeName)

		nodes = append(nodes, NodeResourceUsage{
			NodeName:     nodeName,
			CPUUsage:     status.CPU,
			MemoryTotal:  status.Memory.Total,
			MemoryUsed:   status.Memory.Used,
			StorageTotal: int64(usedStorage),
			StorageUsed:  int64(totalStorage),
		})

		// Add to cluster totals
		response.Cluster.TotalMemoryTotal += status.Memory.Total
		response.Cluster.TotalMemoryUsed += status.Memory.Used
		response.Cluster.TotalStorageTotal += int64(usedStorage)
		response.Cluster.TotalStorageUsed += int64(totalStorage)
		response.Cluster.TotalCPUUsage += status.CPU
	}

	// Get NAS storage and add that to cluster capacity
	usedStorage, totalStorage := getStorage(VirtualResources, "mufasa-proxmox")

	response.Cluster.TotalStorageTotal += int64(usedStorage)
	response.Cluster.TotalStorageUsed += int64(totalStorage)

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
			"error":   "Failed to fetch resource usage for any nodes",
			"details": errors,
		})
		return
	}

	// Success case
	log.Printf("Successfully fetched resource usage for user %s", username)
	c.JSON(http.StatusOK, response)
}

func getNodeStorage(resources *[]VirtualResource, node string) (Used int64, Total int64) {
	var used int64 = 0
	var total int64 = 0

	for _, r := range *resources {
		if r.Type == "storage" && r.NodeName == node &&
			(r.Storage == "local" || r.Storage == "local-lvm") &&
			r.RunningStatus == "available" {
			used += r.Disk
			total += r.MaxDisk
		}
	}
	log.Printf("%s has used %d of its %d local storage", node, used, total)
	return used, total
}

func getStorage(resources *[]VirtualResource, storage string) (Used int64, Total int64) {
	var used int64 = 0
	var total int64 = 0

	for _, r := range *resources {
		if r.Type == "storage" && r.Storage == storage && r.RunningStatus == "available" {
			used = r.Disk
			total = r.MaxDisk
			break
		}
	}
	log.Printf("The cluster has used %d of its %d total storage", used, total)
	return used, total
}

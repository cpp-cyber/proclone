package proxmox

import (
	"fmt"
	"log"
)

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

	return used, total
}

// collectNodeResourceUsage gathers resource usage data for all configured nodes
func (c *Client) collectNodeResourceUsage(resources []VirtualResource) ([]NodeResourceUsage, []string) {
	var nodes []NodeResourceUsage
	var errors []string

	for _, nodeName := range c.Config.Nodes {
		nodeUsage, err := c.getNodeResourceUsage(nodeName, resources)
		if err != nil {
			errorMsg := fmt.Sprintf("Error fetching status for node %s: %v", nodeName, err)
			log.Printf("%s", errorMsg)
			errors = append(errors, errorMsg)
			continue
		}
		nodes = append(nodes, nodeUsage)
	}

	return nodes, errors
}

// getNodeResourceUsage retrieves resource usage for a single node
func (c *Client) getNodeResourceUsage(nodeName string, resources []VirtualResource) (NodeResourceUsage, error) {
	status, err := c.GetNodeStatus(nodeName)
	if err != nil {
		return NodeResourceUsage{}, fmt.Errorf("failed to get node status: %w", err)
	}

	usedStorage, totalStorage := getNodeStorage(&resources, nodeName)

	return NodeResourceUsage{
		Name: nodeName,
		Resources: ResourceUsage{
			CPUUsage:     status.CPU,
			MemoryTotal:  status.Memory.Total,
			MemoryUsed:   status.Memory.Used,
			StorageTotal: int64(totalStorage),
			StorageUsed:  int64(usedStorage),
		},
	}, nil
}

// aggregateClusterResourceUsage calculates cluster-wide resource totals and averages
func (c *Client) aggregateClusterResourceUsage(nodes []NodeResourceUsage, resources []VirtualResource) ResourceUsage {
	cluster := ResourceUsage{}

	// Aggregate node resources
	for _, node := range nodes {
		cluster.MemoryTotal += node.Resources.MemoryTotal
		cluster.MemoryUsed += node.Resources.MemoryUsed
		cluster.StorageTotal += node.Resources.StorageTotal
		cluster.StorageUsed += node.Resources.StorageUsed
		cluster.CPUUsage += node.Resources.CPUUsage
	}

	// Add shared storage (NAS)
	nasUsed, nasTotal := getStorage(&resources, "mufasa-proxmox")
	cluster.StorageTotal += int64(nasTotal)
	cluster.StorageUsed += int64(nasUsed)

	// Calculate average CPU usage
	if len(nodes) > 0 {
		cluster.CPUUsage /= float64(len(nodes))
	}

	return cluster
}

package proxmox

import (
	"fmt"
	"log"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// =================================================
// Public Functions
// =================================================

// GetNodeStatus retrieves detailed status for a specific node
func (s *ProxmoxService) GetNodeStatus(nodeName string) (*ProxmoxNodeStatus, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/status", nodeName),
	}

	var nodeStatus ProxmoxNodeStatus
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &nodeStatus); err != nil {
		return nil, fmt.Errorf("failed to get node status for %s: %w", nodeName, err)
	}

	return &nodeStatus, nil
}

// GetClusterResources retrieves all cluster resources from the Proxmox cluster
func (s *ProxmoxService) GetClusterResources(getParams string) ([]VirtualResource, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/cluster/resources?%s", getParams),
	}

	var resources []VirtualResource
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &resources); err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	return resources, nil
}

// GetClusterResourceUsage retrieves resource usage for the Proxmox cluster
func (s *ProxmoxService) GetClusterResourceUsage() (*ClusterResourceUsageResponse, error) {
	resources, err := s.GetClusterResources("")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	nodes, errors := s.collectNodeResourceUsage(resources)
	cluster := s.aggregateClusterResourceUsage(nodes, resources)

	response := &ClusterResourceUsageResponse{
		Nodes:  nodes,
		Total:  cluster,
		Errors: errors,
	}

	// Return error if all nodes failed
	if len(errors) > 0 && len(nodes) == 0 {
		return nil, fmt.Errorf("failed to fetch resource usage for all nodes: %v", errors)
	}

	return response, nil
}

// FindBestNode finds the node with the most available resources
func (s *ProxmoxService) FindBestNode() (string, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/nodes",
	}

	var nodesResponse []struct {
		Node   string  `json:"node"`
		Status string  `json:"status"`
		CPU    float64 `json:"cpu"`
		MaxCPU int     `json:"maxcpu"`
		Mem    int64   `json:"mem"`
		MaxMem int64   `json:"maxmem"`
	}

	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &nodesResponse); err != nil {
		return "", fmt.Errorf("failed to get nodes: %w", err)
	}

	var bestNode string
	var lowestLoad float64 = 1.0

	for _, node := range nodesResponse {
		if node.Status == "online" {
			// Calculate combined load (CPU + Memory)
			cpuLoad := node.CPU
			memLoad := float64(node.Mem) / float64(node.MaxMem)
			combinedLoad := (cpuLoad + memLoad) / 2

			if combinedLoad < lowestLoad {
				lowestLoad = combinedLoad
				bestNode = node.Node
			}
		}
	}

	if bestNode == "" {
		return "", fmt.Errorf("no online nodes available")
	}

	return bestNode, nil
}

func (s *ProxmoxService) SyncUsers() error {
	return s.syncRealm("users")
}

// =================================================
// Private Functions
// =================================================

// collectNodeResourceUsage gathers resource usage data for all configured nodes
func (s *ProxmoxService) collectNodeResourceUsage(resources []VirtualResource) ([]NodeResourceUsage, []string) {
	var nodes []NodeResourceUsage
	var errors []string

	for _, nodeName := range s.Config.Nodes {
		nodeUsage, err := s.getNodeResourceUsage(nodeName, resources)
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
func (s *ProxmoxService) getNodeResourceUsage(nodeName string, resources []VirtualResource) (NodeResourceUsage, error) {
	status, err := s.GetNodeStatus(nodeName)
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
func (s *ProxmoxService) aggregateClusterResourceUsage(nodes []NodeResourceUsage, resources []VirtualResource) ResourceUsage {
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

func (s *ProxmoxService) syncRealm(scope string) error {
	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/access/domains/%s/sync", s.Config.Realm),
		RequestBody: map[string]string{
			"scope":           scope,                  // Either "users" or "groups"
			"remove-vanished": "acl;properties;entry", // Delete any users/groups that no longer exist in AD
		},
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to sync realm: %w", err)
	}

	return nil
}

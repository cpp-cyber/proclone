package proxmox

import (
	"fmt"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// GetClusterResources retrieves all cluster resources from the Proxmox cluster
func (c *Client) GetClusterResources(getParams string) ([]VirtualResource, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/cluster/resources?%s", getParams),
	}

	var resources []VirtualResource
	if err := c.RequestHelper.MakeRequestAndUnmarshal(req, &resources); err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	return resources, nil
}

// GetNodeStatus retrieves detailed status for a specific node
func (c *Client) GetNodeStatus(nodeName string) (*ProxmoxNodeStatus, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/status", nodeName),
	}

	var nodeStatus ProxmoxNodeStatus
	if err := c.RequestHelper.MakeRequestAndUnmarshal(req, &nodeStatus); err != nil {
		return nil, fmt.Errorf("failed to get node status for %s: %w", nodeName, err)
	}

	return &nodeStatus, nil
}

// GetClusterResourceUsage retrieves resource usage for the Proxmox cluster
func (c *Client) GetClusterResourceUsage() (*ClusterResourceUsageResponse, error) {
	resources, err := c.GetClusterResources("")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	nodes, errors := c.collectNodeResourceUsage(resources)
	cluster := c.aggregateClusterResourceUsage(nodes, resources)

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
func (c *Client) FindBestNode() (string, error) {
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

	if err := c.RequestHelper.MakeRequestAndUnmarshal(req, &nodesResponse); err != nil {
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

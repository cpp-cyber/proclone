package proxmox

import (
	"fmt"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// SetPodVnet configures the VNet for all VMs in a pod
func (c *Client) SetPodVnet(poolName, vnetName string) error {
	// Get all VMs in the pool
	vms, err := c.GetPoolVMs(poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs: %w", err)
	}

	for _, vm := range vms {
		// Update VM network configuration
		reqBody := map[string]string{
			"net0": fmt.Sprintf("virtio,bridge=%s", vnetName),
		}

		req := tools.ProxmoxAPIRequest{
			Method:      "PUT",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/config", vm.NodeName, vm.VmId),
			RequestBody: reqBody,
		}

		_, err := c.RequestHelper.MakeRequest(req)
		if err != nil {
			return fmt.Errorf("failed to update network for VM %d: %w", vm.VmId, err)
		}
	}

	return nil
}

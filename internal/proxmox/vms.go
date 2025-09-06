package proxmox

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// =================================================
// Public Functions
// =================================================

func (c *Client) GetVMs() ([]VirtualResource, error) {
	vms, err := c.GetClusterResources("type=vm")
	if err != nil {
		return nil, err
	}
	return vms, nil
}

func (c *Client) StartVM(node string, vmID int) error {
	return c.vmAction(node, vmID, "start")
}

func (c *Client) StopVM(node string, vmID int) error {
	return c.vmAction(node, vmID, "stop")
}

func (c *Client) ShutdownVM(node string, vmID int) error {
	return c.vmAction(node, vmID, "shutdown")
}

func (c *Client) RebootVM(node string, vmID int) error {
	return c.vmAction(node, vmID, "reboot")
}

func (c *Client) DeleteVM(node string, vmID int) error {
	if err := c.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to delete VM: %w", err)
	}

	return nil
}

func (c *Client) ConvertVMToTemplate(node string, vmID int) error {
	if err := c.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/template", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		if !strings.Contains(err.Error(), "you can't convert a template to a template") {
			return fmt.Errorf("failed to convert VM to template: %w", err)
		}
	}

	return nil
}

func (c *Client) CloneVM(sourceVM VM, newPoolName string) (*VM, error) {
	// Get next available VMID
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/cluster/nextid",
	}

	var nextIDStr string
	if err := c.RequestHelper.MakeRequestAndUnmarshal(req, &nextIDStr); err != nil {
		return nil, fmt.Errorf("failed to get next VMID: %w", err)
	}

	newVMID, err := strconv.Atoi(nextIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid VMID received: %w", err)
	}

	// Find best node for cloning
	bestNode, err := c.FindBestNode()
	if err != nil {
		return nil, fmt.Errorf("failed to find best node: %w", err)
	}

	// Clone VM
	cloneBody := map[string]any{
		"newid":  newVMID,
		"name":   sourceVM.Name,
		"pool":   newPoolName,
		"full":   0, // Linked clone
		"target": bestNode,
	}

	cloneReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/clone", sourceVM.Node, sourceVM.VMID),
		RequestBody: cloneBody,
	}

	_, err = c.RequestHelper.MakeRequest(cloneReq)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate VM clone: %w", err)
	}

	// Wait for clone to complete
	newVM := &VM{
		Node: bestNode,
		VMID: newVMID,
	}

	err = c.WaitForCloneCompletion(newVM, 5*time.Minute) // CLONE_TIMEOUT
	if err != nil {
		return nil, fmt.Errorf("clone operation failed: %w", err)
	}

	return newVM, nil
}

func (c *Client) WaitForCloneCompletion(vm *VM, timeout time.Duration) error {
	start := time.Now()
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for time.Since(start) < timeout {
		// Check VM status
		status, err := c.getVMStatus(vm.Node, vm.VMID)
		if err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		if status == "running" || status == "stopped" {
			// Check if VM is locked (clone in progress)
			configResp, err := c.getVMConfig(vm.Node, vm.VMID)
			if err != nil {
				time.Sleep(backoff)
				backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
				continue
			}

			if configResp.Lock == "" {
				return nil // Clone is complete and VM is not locked
			}
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	return fmt.Errorf("clone operation timed out after %v", timeout)
}

func (c *Client) WaitForDisk(node string, vmid int, maxWait time.Duration) error {
	start := time.Now()

	for time.Since(start) < maxWait {
		time.Sleep(2 * time.Second)

		configResp, err := c.getVMConfig(node, vmid)
		if err != nil {
			continue
		}

		if configResp.HardDisk != "" {
			return nil // Disk is available
		}
	}

	return fmt.Errorf("timeout waiting for VM disks to become available")
}

func (c *Client) WaitForStopped(vm VM) error {
	return c.waitForStatus("stopped", vm)
}

func (c *Client) WaitForRunning(vm VM) error {
	return c.waitForStatus("running", vm)
}

// =================================================
// Private Functions
// =================================================

func (c *Client) vmAction(node string, vmID int, action string) error {
	if err := c.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/%s", node, vmID, action),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to %s VM: %w", action, err)
	}

	return nil
}

func (c *Client) waitForStatus(targetStatus string, vm VM) error {
	timeout := 2 * time.Minute
	start := time.Now()

	for time.Since(start) < timeout {
		currentStatus, err := c.getVMStatus(vm.Node, vm.VMID)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if currentStatus == targetStatus {
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for VM to be %s", targetStatus)
}

func (c *Client) validateVMID(vmID int) error {
	// Get VMs
	vms, err := c.GetClusterResources("type=vm")
	if err != nil {
		return err
	}

	// Check if VMID exists
	for _, vm := range vms {
		if vm.VmId == vmID {
			// Check if VM is in critical pool
			if vm.ResourcePool == c.Config.CriticalPool {
				return fmt.Errorf("VMID %d is in critical pool", vmID)
			}
			return nil
		}
	}

	return fmt.Errorf("VMID %d not found", vmID)
}

func (c *Client) getVMConfig(node string, VMID int) (*VirtualResourceConfig, error) {
	configReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", node, VMID),
	}

	var config VirtualResourceConfig
	if err := c.RequestHelper.MakeRequestAndUnmarshal(configReq, &config); err != nil {
		return nil, fmt.Errorf("failed to get VM config: %w", err)
	}

	return &config, nil
}

func (c *Client) getVMStatus(node string, VMID int) (string, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, VMID),
	}

	var response VirtualResourceStatus
	if err := c.RequestHelper.MakeRequestAndUnmarshal(req, &response); err != nil {
		return "", fmt.Errorf("failed to get VM status: %w", err)
	}

	return response.Status, nil
}

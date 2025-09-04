package proxmox

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

func (c *Client) GetVMs() ([]VirtualResource, error) {
	vms, err := c.GetClusterResources("type=vm")
	if err != nil {
		return nil, err
	}
	return vms, nil
}

func (c *Client) StartVM(node string, vmID int) error {
	if err := c.ValidateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/start", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	return nil
}

func (c *Client) ShutdownVM(node string, vmID int) error {
	if err := c.ValidateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/shutdown", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to shutdown VM: %w", err)
	}

	return nil
}

func (c *Client) RebootVM(node string, vmID int) error {
	if err := c.ValidateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/reboot", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to reboot VM: %w", err)
	}

	return nil
}

func (c *Client) StopVM(node string, vmID int) error {
	if err := c.ValidateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/stop", node, vmID),
	}

	_, err := c.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to stop VM: %w", err)
	}

	return nil
}

// DeleteVM deletes a VM completely
func (c *Client) DeleteVM(node string, vmID int) error {
	if err := c.ValidateVMID(vmID); err != nil {
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
	if err := c.ValidateVMID(vmID); err != nil {
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

// CloneVM clones a VM to a new pool
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

// WaitForCloneCompletion waits for a clone operation to complete
func (c *Client) WaitForCloneCompletion(vm *VM, timeout time.Duration) error {
	start := time.Now()
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for time.Since(start) < timeout {
		// Check VM status
		req := tools.ProxmoxAPIRequest{
			Method:   "GET",
			Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/current", vm.Node, vm.VMID),
		}

		data, err := c.RequestHelper.MakeRequest(req)
		if err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		var statusResponse struct {
			Status string `json:"status"`
		}

		if err := json.Unmarshal(data, &statusResponse); err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		if statusResponse.Status == "running" || statusResponse.Status == "stopped" {
			// Check if VM is locked (clone in progress)
			configReq := tools.ProxmoxAPIRequest{
				Method:   "GET",
				Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", vm.Node, vm.VMID),
			}

			configData, err := c.RequestHelper.MakeRequest(configReq)
			if err != nil {
				time.Sleep(backoff)
				backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
				continue
			}

			var configResp ConfigResponse
			if err := json.Unmarshal(configData, &configResp); err != nil {
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

// WaitForDiskAvailability waits for VM disks to become available
func (c *Client) WaitForDiskAvailability(node string, vmid int, maxWait time.Duration) error {
	start := time.Now()

	for time.Since(start) < maxWait {
		time.Sleep(2 * time.Second)

		req := tools.ProxmoxAPIRequest{
			Method:   "GET",
			Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid),
		}

		data, err := c.RequestHelper.MakeRequest(req)
		if err != nil {
			continue
		}

		var configResp ConfigResponse
		if err := json.Unmarshal(data, &configResp); err != nil {
			continue
		}

		if configResp.HardDisk != "" {
			return nil
		}
	}

	return fmt.Errorf("timeout waiting for VM disks to become available")
}

// WaitForRunning waits for a VM to be in running state
func (c *Client) WaitForRunning(vm VM) error {
	timeout := 2 * time.Minute
	start := time.Now()

	for time.Since(start) < timeout {
		req := tools.ProxmoxAPIRequest{
			Method:   "GET",
			Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/current", vm.Node, vm.VMID),
		}

		data, err := c.RequestHelper.MakeRequest(req)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var statusResponse struct {
			Status string `json:"status"`
		}

		if err := json.Unmarshal(data, &statusResponse); err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if statusResponse.Status == "running" {
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for VM to be running")
}

// WaitForStopped waits for a VM to be in stopped state
func (c *Client) WaitForStopped(vm VM) error {
	timeout := 2 * time.Minute
	start := time.Now()

	for time.Since(start) < timeout {
		req := tools.ProxmoxAPIRequest{
			Method:   "GET",
			Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/current", vm.Node, vm.VMID),
		}

		data, err := c.RequestHelper.MakeRequest(req)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		var statusResponse struct {
			Status string `json:"status"`
		}

		if err := json.Unmarshal(data, &statusResponse); err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if statusResponse.Status == "stopped" {
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timeout waiting for VM to be stopped")
}

func (c *Client) ValidateVMID(vmID int) error {
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

package proxmox

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// =================================================
// Public Functions
// =================================================

func (s *ProxmoxService) GetVMs() ([]VirtualResource, error) {
	vms, err := s.GetClusterResources("type=vm")
	if err != nil {
		return nil, err
	}
	return vms, nil
}

func (s *ProxmoxService) StartVM(node string, vmID int) error {
	return s.vmAction("start", node, vmID)
}

func (s *ProxmoxService) StopVM(node string, vmID int) error {
	return s.vmAction("stop", node, vmID)
}

func (s *ProxmoxService) ShutdownVM(node string, vmID int) error {
	return s.vmAction("shutdown", node, vmID)
}

func (s *ProxmoxService) RebootVM(node string, vmID int) error {
	return s.vmAction("reboot", node, vmID)
}

func (s *ProxmoxService) DeleteVM(node string, vmID int) error {
	if err := s.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d", node, vmID),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to delete VM: %w", err)
	}

	return nil
}

func (s *ProxmoxService) ConvertVMToTemplate(node string, vmID int) error {
	if err := s.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/template", node, vmID),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		if !strings.Contains(err.Error(), "you can't convert a template to a template") {
			return fmt.Errorf("failed to convert VM to template: %w", err)
		}
	}

	return nil
}

func (s *ProxmoxService) CloneVM(sourceVM VM, newPoolName string) (*VM, error) {
	// Get next available VMID
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/cluster/nextid",
	}

	var nextIDStr string
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &nextIDStr); err != nil {
		return nil, fmt.Errorf("failed to get next VMID: %w", err)
	}

	newVMID, err := strconv.Atoi(nextIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid VMID received: %w", err)
	}

	// Find best node for cloning
	bestNode, err := s.FindBestNode()
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

	_, err = s.RequestHelper.MakeRequest(cloneReq)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate VM clone: %w", err)
	}

	// Wait for clone to complete
	newVM := &VM{
		Node: bestNode,
		VMID: newVMID,
	}

	err = s.WaitForCloneCompletion(newVM, 5*time.Minute) // CLONE_TIMEOUT
	if err != nil {
		return nil, fmt.Errorf("clone operation failed: %w", err)
	}

	return newVM, nil
}

func (s *ProxmoxService) CloneVMWithConfig(req VMCloneRequest) error {
	// Clone VM
	cloneBody := map[string]any{
		"newid":  req.NewVMID,
		"name":   req.SourceVM.Name,
		"pool":   req.PoolName,
		"full":   0, // Linked clone
		"target": req.TargetNode,
	}

	cloneReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/clone", req.SourceVM.Node, req.SourceVM.VMID),
		RequestBody: cloneBody,
	}

	_, err := s.RequestHelper.MakeRequest(cloneReq)
	if err != nil {
		return fmt.Errorf("failed to initiate VM clone: %w", err)
	}

	return nil
}

func (s *ProxmoxService) WaitForCloneCompletion(vm *VM, timeout time.Duration) error {
	start := time.Now()
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for time.Since(start) < timeout {
		// Check VM status
		status, err := s.getVMStatus(vm.Node, vm.VMID)
		if err != nil {
			time.Sleep(backoff)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		if status == "running" || status == "stopped" {
			// Check if VM is locked (clone in progress)
			configResp, err := s.getVMConfig(vm.Node, vm.VMID)
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

func (s *ProxmoxService) WaitForDisk(node string, vmid int, maxWait time.Duration) error {
	start := time.Now()

	for time.Since(start) < maxWait {
		time.Sleep(2 * time.Second)

		configResp, err := s.getVMConfig(node, vmid)
		if err != nil {
			continue
		}

		if configResp.HardDisk != "" {
			pendingReq := tools.ProxmoxAPIRequest{
				Method:   "GET",
				Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/pending", node, vmid),
			}

			pendingResponse, err := s.RequestHelper.MakeRequest(pendingReq)
			log.Printf("Pending response for VMID %d on node %s: %s", vmid, node, string(pendingResponse))
			if err != nil && strings.Contains(err.Error(), "does not exist") {
				log.Printf("Disk for VMID %d on node %s not ready yet: %v", vmid, node, err)
				continue // Disk not synced yet
			}

			return nil // Disk is available
		}
	}

	return fmt.Errorf("timeout waiting for VM disks to become available")
}

func (s *ProxmoxService) WaitForStopped(vm VM) error {
	return s.waitForStatus("stopped", vm)
}

func (s *ProxmoxService) WaitForRunning(vm VM) error {
	return s.waitForStatus("running", vm)
}

func (s *ProxmoxService) GetNextVMIDs(num int) ([]int, error) {
	// Get VMs
	resources, err := s.GetClusterResources("type=vm")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	// Iterate thought and find the highest VMID under 4000
	highestID := 100
	for _, res := range resources {
		if res.VmId > highestID && res.VmId < 4000 {
			highestID = res.VmId
		}
	}

	// Generate the next num VMIDs
	var vmIDs []int
	for i := 1; i <= num; i++ {
		vmIDs = append(vmIDs, highestID+i)
	}

	return vmIDs, nil
}

// =================================================
// Private Functions
// =================================================

func (s *ProxmoxService) vmAction(action string, node string, vmID int) error {
	if err := s.validateVMID(vmID); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/%s", node, vmID, action),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to %s VM: %w", action, err)
	}

	return nil
}

func (s *ProxmoxService) waitForStatus(targetStatus string, vm VM) error {
	timeout := 2 * time.Minute
	start := time.Now()

	for time.Since(start) < timeout {
		currentStatus, err := s.getVMStatus(vm.Node, vm.VMID)
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

func (s *ProxmoxService) validateVMID(vmID int) error {
	// Get VMs
	vms, err := s.GetClusterResources("type=vm")
	if err != nil {
		return err
	}

	// Check if VMID exists
	for _, vm := range vms {
		if vm.VmId == vmID {
			// Check if VM is in critical pool
			if vm.ResourcePool == s.Config.CriticalPool {
				return fmt.Errorf("VMID %d is in critical pool", vmID)
			}
			return nil
		}
	}

	return fmt.Errorf("VMID %d not found", vmID)
}

func (s *ProxmoxService) getVMConfig(node string, VMID int) (*VirtualResourceConfig, error) {
	configReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", node, VMID),
	}

	var config VirtualResourceConfig
	if err := s.RequestHelper.MakeRequestAndUnmarshal(configReq, &config); err != nil {
		return nil, fmt.Errorf("failed to get VM config: %w", err)
	}

	return &config, nil
}

func (s *ProxmoxService) getVMStatus(node string, VMID int) (string, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, VMID),
	}

	var response VirtualResourceStatus
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &response); err != nil {
		return "", fmt.Errorf("failed to get VM status: %w", err)
	}

	return response.Status, nil
}

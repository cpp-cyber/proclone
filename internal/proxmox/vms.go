package proxmox

import (
	"fmt"
	"log"
	"slices"
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
		return []VirtualResource{}, err
	}
	return vms, nil
}

func (s *ProxmoxService) GetVMTemplates() ([]VirtualResource, error) {
	vms, err := s.GetClusterResources("type=vm")
	if err != nil {
		return []VirtualResource{}, err
	}

	var templates []VirtualResource
	for _, vm := range vms {
		if vm.ResourcePool == s.Config.VMTemplatePool {
			templates = append(templates, vm)
		}
	}

	return templates, nil
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

func (s *ProxmoxService) GetVMSnapshots(node string, vmID int) ([]VMSnapshot, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/snapshot", node, vmID),
	}

	var snapshots []VMSnapshot
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to get snapshots for VMID %d on node %s: %w", vmID, node, err)
	}

	return snapshots, nil
}

func (s *ProxmoxService) DeleteVMSnapshot(node string, vmID int, snapshotName string) error {
	req := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/snapshot/%s", node, vmID, snapshotName),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot %s for VMID %d on node %s: %w", snapshotName, vmID, node, err)
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

func (s *ProxmoxService) CloneVM(req VMCloneRequest) error {
	log.Printf("[kayhon] CloneVM: SourceVM={Name=%s, VMID=%d, Node=%s}, NewVMID=%d, Pool=%s, TargetNode=%s, Full=%d",
		req.SourceVM.Name, req.SourceVM.VMID, req.SourceVM.Node, req.NewVMID, req.PoolName, req.TargetNode, req.Full)

	// Clone VM
	cloneBody := map[string]any{
		"newid":  req.NewVMID,
		"name":   req.SourceVM.Name,
		"pool":   req.PoolName,
		"full":   req.Full,
		"target": req.TargetNode,
	}

	cloneReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/clone", req.SourceVM.Node, req.SourceVM.VMID),
		RequestBody: cloneBody,
	}

	_, err := s.RequestHelper.MakeRequest(cloneReq)
	if err != nil {
		log.Printf("[kayhon] CloneVM FAILED: SourceVMID=%d -> NewVMID=%d, error: %v", req.SourceVM.VMID, req.NewVMID, err)
		return fmt.Errorf("failed to initiate VM clone: %w", err)
	}

	log.Printf("[kayhon] CloneVM initiated successfully: SourceVMID=%d -> NewVMID=%d", req.SourceVM.VMID, req.NewVMID)
	return nil
}

func (s *ProxmoxService) cloneVMWithUPID(req VMCloneRequest) (string, error) {
	// Clone VM
	cloneBody := map[string]any{
		"newid":  req.NewVMID,
		"name":   req.SourceVM.Name,
		"pool":   req.PoolName,
		"full":   req.Full,
		"target": req.TargetNode,
	}

	cloneReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/clone", req.SourceVM.Node, req.SourceVM.VMID),
		RequestBody: cloneBody,
	}

	var upid string
	if err := s.RequestHelper.MakeRequestAndUnmarshal(cloneReq, &upid); err != nil {
		return "", fmt.Errorf("failed to initiate VM clone: %w", err)
	}

	return upid, nil
}

func (s *ProxmoxService) WaitForDisk(node string, vmID int, maxWait time.Duration) error {
	start := time.Now()
	log.Printf("[kayhon] WaitForDisk START: Node=%s, VMID=%d, MaxWait=%s", node, vmID, maxWait)

	for time.Since(start) < maxWait {
		time.Sleep(2 * time.Second)

		configResp, err := s.getVMConfig(node, vmID)
		if err != nil {
			log.Printf("[kayhon] WaitForDisk: Failed to get VM config for VMID=%d: %v (elapsed: %s)", vmID, err, time.Since(start))
			continue
		}

		log.Printf("[kayhon] WaitForDisk VM config for VMID=%d: Name=%s, HardDisk=%s, Lock=%s, Net0=%s, Net1=%s",
			vmID, configResp.Name, configResp.HardDisk, configResp.Lock, configResp.Net0, configResp.Net1)

		if configResp.HardDisk != "" && configResp.Name != "" {
			log.Printf("[kayhon] WaitForDisk: VM config has HardDisk and Name, checking storage content for VMID=%d", vmID)

			pendingReq := tools.ProxmoxAPIRequest{
				Method:   "GET",
				Endpoint: fmt.Sprintf("/nodes/%s/storage/%s/content?vmid=%d", s.Config.Nodes[0], s.Config.StorageID, vmID),
			}

			var diskResponse []PendingDiskResponse
			err := s.RequestHelper.MakeRequestAndUnmarshal(pendingReq, &diskResponse)
			if err != nil || len(diskResponse) == 0 {
				log.Printf("[kayhon] WaitForDisk: Error or empty disk response for VMID=%d: err=%v, count=%d", vmID, err, len(diskResponse))
				continue
			}

			log.Printf("[kayhon] WaitForDisk: Disk response for VMID=%d: %d disk(s)", vmID, len(diskResponse))
			for i, disk := range diskResponse {
				log.Printf("[kayhon]   Disk %d: Used=%d, Size=%d", i, disk.Used, disk.Size)
			}

			// Iterate through all disks, if all have valid Used and Size (not 0) consider available
			allAvailable := true
			for _, disk := range diskResponse {
				if disk.Size == 0 {
					allAvailable = false
					break
				}
			}

			if allAvailable {
				log.Printf("[kayhon] WaitForDisk READY: VMID=%d, all disks available (elapsed: %s)", vmID, time.Since(start))
				return nil // Disk is available
			}
			log.Printf("[kayhon] WaitForDisk: Not all disks ready for VMID=%d, retrying...", vmID)
		} else {
			log.Printf("[kayhon] WaitForDisk: VM config incomplete for VMID=%d (HardDisk=%q, Name=%q), retrying...", vmID, configResp.HardDisk, configResp.Name)
		}
	}

	log.Printf("[kayhon] WaitForDisk TIMEOUT: VMID=%d after %s", vmID, maxWait)
	return fmt.Errorf("timeout waiting for VM disks to become available")
}

func (s *ProxmoxService) WaitForStopped(node string, vmID int) error {
	return s.waitForStatus("stopped", node, vmID)
}

func (s *ProxmoxService) WaitForRunning(node string, vmID int) error {
	return s.waitForStatus("running", node, vmID)
}

func (s *ProxmoxService) GetNextVMIDs(num int) ([]int, error) {
	// Get VMs
	resources, err := s.GetClusterResources("type=vm")
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster resources: %w", err)
	}

	var usedVMIDs []int
	for _, vm := range resources {
		usedVMIDs = append(usedVMIDs, vm.VmId)
	}
	// Sort VMIDs from lowest to highest
	slices.Sort(usedVMIDs)

	// Iterate through and find the lowest available VMID range that has enough space based on num
	lowestID := usedVMIDs[len(usedVMIDs)-1] // Set to highest existing VMID by default
	prevID := usedVMIDs[0]                  // Start at the lowest existing VMID
	for _, vmID := range usedVMIDs[1 : len(usedVMIDs)-1] {
		if (vmID - prevID) > num {
			log.Printf("Found available VMID range between %d and %d", prevID, vmID)
			lowestID = prevID
			break
		}
		prevID = vmID
	}

	// Generate the next num VMIDs
	var vmIDs []int
	for i := 1; i <= num; i++ {
		vmIDs = append(vmIDs, lowestID+i)
	}

	return vmIDs, nil
}

func (s *ProxmoxService) WaitForLock(node string, vmID int) error {
	timeout := 1 * time.Minute
	start := time.Now()
	log.Printf("[kayhon] WaitForLock START: Node=%s, VMID=%d, Timeout=%s", node, vmID, timeout)

	for time.Since(start) < timeout {
		config, err := s.getVMConfig(node, vmID)
		if err != nil {
			log.Printf("[kayhon] WaitForLock: Failed to get VM config for VMID=%d: %v", vmID, err)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[kayhon] WaitForLock VM config for VMID=%d: Name=%s, Lock=%s, HardDisk=%s, Net0=%s, Net1=%s",
			vmID, config.Name, config.Lock, config.HardDisk, config.Net0, config.Net1)

		if config.Lock == "" {
			log.Printf("[kayhon] WaitForLock CLEARED: VMID=%d (elapsed: %s)", vmID, time.Since(start))
			return nil // No lock
		}

		log.Printf("[kayhon] WaitForLock: VMID=%d still locked (lock=%s), waiting...", vmID, config.Lock)
		time.Sleep(5 * time.Second)
	}

	log.Printf("[kayhon] WaitForLock TIMEOUT: VMID=%d after %s", vmID, timeout)
	return fmt.Errorf("timeout waiting for VM lock to be cleared")
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

func (s *ProxmoxService) waitForStatus(targetStatus string, node string, vmID int) error {
	timeout := 2 * time.Minute
	start := time.Now()
	log.Printf("[kayhon] waitForStatus START: Node=%s, VMID=%d, TargetStatus=%s, Timeout=%s", node, vmID, targetStatus, timeout)

	for time.Since(start) < timeout {
		currentStatus, err := s.getVMStatus(node, vmID)
		if err != nil {
			log.Printf("[kayhon] waitForStatus: Failed to get status for VMID=%d: %v", vmID, err)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[kayhon] waitForStatus: VMID=%d currentStatus=%s, targetStatus=%s", vmID, currentStatus, targetStatus)

		if currentStatus == targetStatus {
			log.Printf("[kayhon] waitForStatus REACHED: VMID=%d is now %s (elapsed: %s)", vmID, targetStatus, time.Since(start))
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	log.Printf("[kayhon] waitForStatus TIMEOUT: VMID=%d never reached %s after %s", vmID, targetStatus, timeout)
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
		log.Printf("[kayhon] getVMConfig FAILED: Node=%s, VMID=%d, error: %v", node, VMID, err)
		return nil, fmt.Errorf("failed to get VM config: %w", err)
	}

	log.Printf("[kayhon] getVMConfig: Node=%s, VMID=%d -> Name=%s, HardDisk=%s, Lock=%s, Net0=%s, Net1=%s",
		node, VMID, config.Name, config.HardDisk, config.Lock, config.Net0, config.Net1)
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

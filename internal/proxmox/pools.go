package proxmox

import (
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

func (s *ProxmoxService) GetPoolVMs(poolName string) ([]VirtualResource, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/pools/%s", poolName),
	}

	var poolResponse struct {
		Members []VirtualResource `json:"members"`
	}
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &poolResponse); err != nil {
		return nil, fmt.Errorf("failed to get pool VMs: %w", err)
	}

	// Filter for VMs only (type=qemu)
	var vms []VirtualResource
	for _, member := range poolResponse.Members {
		if member.Type == "qemu" {
			vms = append(vms, member)
		}
	}

	return vms, nil
}

func (s *ProxmoxService) CreateNewPool(poolName string) error {
	reqBody := map[string]string{
		"poolid": poolName,
	}

	req := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    "/pools",
		RequestBody: reqBody,
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to create pool %s: %w", poolName, err)
	}

	return nil
}

func (s *ProxmoxService) SetPoolPermission(poolName string, targetName string, isGroup bool) error {
	reqBody := map[string]any{
		"path":      fmt.Sprintf("/pool/%s", poolName),
		"roles":     "PVEVMUser,PVEPoolUser",
		"propagate": true,
	}

	if isGroup {
		reqBody["groups"] = fmt.Sprintf("%s-%s", targetName, s.Config.Realm)
	} else {
		reqBody["users"] = fmt.Sprintf("%s@%s", targetName, s.Config.Realm)
	}

	req := tools.ProxmoxAPIRequest{
		Method:      "PUT",
		Endpoint:    "/access/acl",
		RequestBody: reqBody,
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to set pool permissions: %w", err)
	}

	return nil
}

func (s *ProxmoxService) DeletePool(poolName string) error {
	req := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/pools/%s", poolName),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to delete pool %s: %w", poolName, err)
	}

	log.Printf("Successfully deleted pool: %s", poolName)
	return nil
}

func (s *ProxmoxService) GetTemplatePools() ([]string, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/pools",
	}

	var poolResponse []struct {
		Name string `json:"poolid"`
	}
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &poolResponse); err != nil {
		return nil, fmt.Errorf("failed to get template pools: %w", err)
	}

	var templatePools []string
	for _, pool := range poolResponse {
		if strings.HasPrefix(pool.Name, "kamino_template_") {
			templatePools = append(templatePools, pool.Name)
		}
	}

	return templatePools, nil
}

func (s *ProxmoxService) IsPoolEmpty(poolName string) (bool, error) {
	poolVMs, err := s.GetPoolVMs(poolName)
	if err != nil {
		return false, fmt.Errorf("failed to check if pool %s is empty: %w", poolName, err)
	}

	// Count only QEMU VMs (ignore other resource types)
	vmCount := 0
	for _, vm := range poolVMs {
		if vm.Type == "qemu" {
			vmCount++
		}
	}

	return vmCount == 0, nil
}

func (s *ProxmoxService) WaitForPoolEmpty(poolName string, timeout time.Duration) error {
	start := time.Now()
	backoff := 2 * time.Second
	maxBackoff := 30 * time.Second

	for time.Since(start) < timeout {
		poolVMs, err := s.GetPoolVMs(poolName)
		if err != nil {
			// If we can't get pool VMs, pool might be deleted or empty
			log.Printf("Error checking pool %s (might be deleted): %v", poolName, err)
			return nil
		}

		if len(poolVMs) == 0 {
			log.Printf("Pool %s is now empty", poolName)
			return nil
		}

		log.Printf("Pool %s still contains %d VMs, waiting...", poolName, len(poolVMs))
		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	return fmt.Errorf("timeout waiting for pool %s to become empty after %v", poolName, timeout)
}

func (s *ProxmoxService) GetNextPodID(minPodID int, maxPodID int) (string, int, error) {
	// Get all existing pools
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/pools",
	}

	var poolsResponse []struct {
		PoolID string `json:"poolid"`
	}
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &poolsResponse); err != nil {
		return "", 0, fmt.Errorf("failed to get existing pools: %w", err)
	}

	// Extract pod IDs from existing pools
	var usedIDs []int
	for _, pool := range poolsResponse {
		if len(pool.PoolID) >= 4 {
			if id, err := strconv.Atoi(pool.PoolID[:4]); err == nil {
				if id >= minPodID && id <= maxPodID {
					usedIDs = append(usedIDs, id)
				}
			}
		}
	}

	sort.Ints(usedIDs)

	// Find first available ID
	for i := minPodID; i <= maxPodID; i++ {
		found := slices.Contains(usedIDs, i)
		if !found {
			return fmt.Sprintf("%04d", i), i - 1000, nil
		}
	}

	return "", 0, fmt.Errorf("no available pod IDs in range 1000-1255")
}

func (s *ProxmoxService) GetNextPodIDs(minPodID int, maxPodID int, num int) ([]string, []int, error) {
	// Get all existing pools
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/pools",
	}

	var poolsResponse []struct {
		PoolID string `json:"poolid"`
	}
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &poolsResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to get existing pools: %w", err)
	}

	// Extract pod IDs from existing pools
	var usedIDs []int
	for _, pool := range poolsResponse {
		if len(pool.PoolID) >= 4 {
			if id, err := strconv.Atoi(pool.PoolID[:4]); err == nil {
				if id >= minPodID && id <= maxPodID {
					usedIDs = append(usedIDs, id)
				}
			}
		}
	}

	sort.Ints(usedIDs)

	// Find available IDs
	var podIDs []string
	var adjustedIDs []int

	for i := minPodID; i <= maxPodID && len(podIDs) < num; i++ {
		found := slices.Contains(usedIDs, i)
		if !found {
			podIDs = append(podIDs, fmt.Sprintf("%04d", i))
			adjustedIDs = append(adjustedIDs, i-1000)
		}
	}

	if len(podIDs) < num {
		return nil, nil, fmt.Errorf("only found %d available pod IDs out of %d requested in range %d-%d", len(podIDs), num, minPodID, maxPodID)
	}

	return podIDs, adjustedIDs, nil
}

func (s *ProxmoxService) CreateTemplatePool(creator string, name string, addRouter bool, vms []VM) error {
	// 1. Create pool in proxmox with specific name and "kamino_template_" prefix
	poolName := fmt.Sprintf("kamino_template_%s", name)
	if err := s.CreateNewPool(poolName); err != nil {
		return err
	}

	if err := s.SetPoolPermission(poolName, creator, false); err != nil {
		return err
	}

	if addRouter == false && len(vms) == 0 {
		return nil
	}

	// 2. Get VMIDs
	numVMs := len(vms)
	if addRouter {
		numVMs++
	}

	vmIDs, err := s.GetNextVMIDs(numVMs)
	if err != nil {
		return err
	}

	// 3. Find best node to clone to
	bestNode, err := s.FindBestNode()
	if err != nil {
		return err
	}

	// 4. If addRouter is true, clone router from Config
	var router VM
	var routerCloneReq VMCloneRequest
	var routerVMID int

	if addRouter {
		router = VM{
			Name: s.Config.RouterName,
			Node: s.Config.RouterNode,
			VMID: s.Config.RouterVMID,
		}

		// Save router VMID before removing it from the list
		routerVMID = vmIDs[0]

		routerCloneReq = VMCloneRequest{
			SourceVM:   router,
			PoolName:   poolName,
			NewVMID:    routerVMID,
			TargetNode: bestNode,
		}

		// Remove the first VMID from the list
		vmIDs = vmIDs[1:]

		if err := s.CloneVM(routerCloneReq); err != nil {
			return err
		}
	}

	// 5. Clone specified templates to newly created pool with the specified names
	for i, vm := range vms {
		vmCloneReq := VMCloneRequest{
			SourceVM:   vm,
			PoolName:   poolName,
			NewVMID:    vmIDs[i],
			TargetNode: bestNode,
		}

		if err := s.CloneVM(vmCloneReq); err != nil {
			return err
		}
	}

	// Return with no error if addRouter is false since all other operations below have to do with routing
	if !addRouter {
		return nil
	}

	// 8. Wait for all VM clone operations to complete before configuring VNets
	log.Printf("Waiting for VMs in pool %s to be available", poolName)
	time.Sleep(2 * time.Second)

	// First wait for all VMs to appear in the pool
	for retries := range 30 {
		poolVMs, err := s.GetPoolVMs(poolName)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if len(poolVMs) >= numVMs {
			log.Printf("Pool %s has %d VMs (expected %d) - all VMs present", poolName, len(poolVMs), numVMs)
			break
		}

		log.Printf("Pool %s has %d VMs, waiting for %d (retry %d/30)", poolName, len(poolVMs), numVMs, retries+1)
		time.Sleep(2 * time.Second)
	}

	// Wait for all VM locks to be released
	log.Printf("Waiting for all VM clone operations to complete (checking locks)")
	poolVMs, err := s.GetPoolVMs(poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs after waiting: %w", err)
	}

	for _, vm := range poolVMs {
		log.Printf("Waiting for VM %d (%s) lock to be released", vm.VmId, vm.Name)
		if err := s.WaitForLock(vm.NodeName, vm.VmId); err != nil {
			log.Printf("Warning: timeout waiting for VM %d lock, continuing anyway: %v", vm.VmId, err)
		}
	}

	log.Printf("All clone operations complete for pool %s", poolName)

	// 9. Configure VNet for all VMs
	// Get number of template pools
	templatePools, err := s.GetTemplatePools()
	if err != nil {
		return fmt.Errorf("failed to get template pools: %w", err)
	}

	// Calculate the template ID to get the VNet name
	templateID := len(templatePools) % 10
	vnet := fmt.Sprintf("templ%d", templateID)

	log.Printf("Configuring VNet %s for pool %s", vnet, poolName)
	err = s.SetPodVnet(poolName, vnet)
	if err != nil {
		return fmt.Errorf("failed to set VNet for pool %s: %w", poolName, err)
	}

	// 10. Start router and wait for it to be available
	if addRouter {
		log.Printf("Starting router VM (VMID: %d) on node %s", routerVMID, bestNode)

		// Wait for router disk to be available
		log.Printf("Waiting for router disk to be available")
		err = s.WaitForDisk(bestNode, routerVMID, 2*time.Minute)
		if err != nil {
			return fmt.Errorf("router disk unavailable: %w", err)
		}

		// Start the router
		log.Printf("Starting router VM")
		err = s.StartVM(bestNode, routerVMID)
		if err != nil {
			return fmt.Errorf("failed to start router VM: %w", err)
		}

		// Wait for router to be running
		log.Printf("Waiting for router VM to be running")
		err = s.WaitForRunning(bestNode, routerVMID)
		if err != nil {
			return fmt.Errorf("router VM failed to start: %w", err)
		}

		log.Printf("Router VM is now running")
	}

	// 11. Run config scripts on router
	// Determine router type
	routerType, err := s.GetRouterType(router)
	if err != nil {
		return fmt.Errorf("failed to get router type: %v", err)
	}

	// Calculate the third octect
	octect := 254 - templateID

	err = s.ConfigurePodRouter(octect, bestNode, router.VMID, routerType)
	if err != nil {
		return fmt.Errorf("failed to configure router for %s: %v", routerType, err)
	}

	log.Printf("Successfully created template pool %s with %d VMs", poolName, len(vms))
	if addRouter {
		log.Printf("Router VM included and started")
	}

	return nil
}

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

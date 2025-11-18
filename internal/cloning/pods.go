package cloning

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cpp-cyber/proclone/internal/proxmox"
)

func (cs *CloningService) GetPods(username string) ([]Pod, error) {
	// Get user's groups
	groups, err := cs.ProxmoxService.GetUserGroups(username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Build regex pattern to match username or any of their group names
	groupsWithUser := append(groups, username)
	regexPattern := fmt.Sprintf(`(?i)1[0-9]{3}_.*_(%s)`, strings.Join(groupsWithUser, "|"))

	// Get pods based on regex pattern
	pods, err := cs.MapVirtualResourcesToPods(regexPattern)
	if err != nil {
		return nil, err
	}
	return pods, nil
}

func (cs *CloningService) AdminGetPods() ([]Pod, error) {
	pods, err := cs.MapVirtualResourcesToPods(`1[0-9]{3}_.*`)
	if err != nil {
		return nil, err
	}
	return pods, nil
}

func (cs *CloningService) MapVirtualResourcesToPods(regex string) ([]Pod, error) {
	// Get cluster resources
	resources, err := cs.ProxmoxService.GetClusterResources("")
	if err != nil {
		return nil, err
	}

	podMap := make(map[string]*Pod)
	reg := regexp.MustCompile(regex)

	// Iterate over cluster resources, this works because proxmox displays pools before VMs
	for _, r := range resources {
		if r.Type == "pool" && reg.MatchString(r.ResourcePool) {
			name := r.ResourcePool
			podMap[name] = &Pod{
				Name: name,
				VMs:  []proxmox.VirtualResource{},
			}
		}
		if r.Type == "qemu" && reg.MatchString(r.ResourcePool) {
			if pod, ok := podMap[r.ResourcePool]; ok {
				pod.VMs = append(pod.VMs, r)
			}
		}
	}

	// Convert map to slice
	var pods []Pod
	for _, pod := range podMap {
		pods = append(pods, *pod)
	}

	return pods, nil
}

func (cs *CloningService) ValidateCloneRequest(templateName string, username string) (bool, error) {
	podPools, err := cs.AdminGetPods()
	if err != nil {
		return false, fmt.Errorf("failed to get deployed pods: %w", err)
	}

	var alreadyDeployed = false
	var numDeployments = 0

	for _, pod := range podPools {
		// Remove the Pod ID number and _ to compare
		if !alreadyDeployed && strings.EqualFold(pod.Name[5:], templateName) {
			alreadyDeployed = true
		}

		if strings.Contains(strings.ToLower(pod.Name), strings.ToLower(username)) {
			numDeployments++
		}
	}

	// Valid if not already deployed and user has less than 5 deployments
	var isValidCloneRequest = !alreadyDeployed && numDeployments < 5

	return isValidCloneRequest, nil
}

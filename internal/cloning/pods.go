package cloning

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cpp-cyber/proclone/internal/proxmox"
)

func (cm *CloningManager) GetPods(username string) ([]Pod, error) {
	// Get User DN
	userDN, err := cm.LDAPService.GetUserDN(username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user DN: %w", err)
	}

	// Get user's groups
	groups, err := cm.LDAPService.GetUserGroups(userDN)
	if err != nil {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Build regex pattern to match username or any of their group names
	regexPattern := fmt.Sprintf(`1[0-9]{3}_.*_(%s|%s)`, username, strings.Join(groups, "|"))

	// Get pods based on regex pattern
	pods, err := cm.MapVirtualResourcesToPods(regexPattern)
	if err != nil {
		return nil, err
	}
	return pods, nil
}

func (cm *CloningManager) GetAllPods() ([]Pod, error) {
	pods, err := cm.MapVirtualResourcesToPods(`1[0-9]{3}_.*`)
	if err != nil {
		return nil, err
	}
	return pods, nil
}

func (cm *CloningManager) MapVirtualResourcesToPods(regex string) ([]Pod, error) {
	// Get cluster resources
	resources, err := cm.ProxmoxService.GetClusterResources("")
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

func (cm *CloningManager) IsDeployed(templateName string) (bool, error) {
	podPools, err := cm.GetAllPods()
	if err != nil {
		return false, fmt.Errorf("failed to get pod pools: %w", err)
	}

	for _, pod := range podPools {
		// Remove the Pod ID number and _ to compare
		if pod.Name[5:] == templateName {
			return true, nil
		}
	}

	return false, nil
}

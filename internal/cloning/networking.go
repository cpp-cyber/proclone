package cloning

import (
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// configurePodRouter configures the pod router with proper networking settings
func (cs *CloningService) configurePodRouter(podNumber int, node string, vmid int) error {
	// Wait for router agent to be pingable
	statusReq := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/agent/ping", node, vmid),
	}

	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			return fmt.Errorf("router qemu agent timed out")
		}

		_, err := cs.ProxmoxService.GetRequestHelper().MakeRequest(statusReq)
		if err == nil {
			break // Agent is responding
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// Configure router WAN IP to have correct third octet using qemu agent API call
	reqBody := map[string]any{
		"command": []string{
			cs.Config.WANScriptPath,
			fmt.Sprintf("%s%d.1", cs.Config.WANIPBase, podNumber),
		},
	}

	execReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
		RequestBody: reqBody,
	}

	_, err := cs.ProxmoxService.GetRequestHelper().MakeRequest(execReq)
	if err != nil {
		return fmt.Errorf("failed to make IP change request: %v", err)
	}

	// Send agent exec request to change VIP subnet
	vipReqBody := map[string]any{
		"command": []string{
			cs.Config.VIPScriptPath,
			fmt.Sprintf("%s%d.0", cs.Config.WANIPBase, podNumber),
		},
	}

	vipExecReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
		RequestBody: vipReqBody,
	}

	_, err = cs.ProxmoxService.GetRequestHelper().MakeRequest(vipExecReq)
	if err != nil {
		return fmt.Errorf("failed to make VIP change request: %v", err)
	}

	return nil
}

func (cs *CloningService) SetPodVnet(poolName string, vnetName string) error {
	// Get all VMs in the pool
	vms, err := cs.ProxmoxService.GetPoolVMs(poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs: %w", err)
	}

	routerRegex := regexp.MustCompile(`(?i).*(router|pfsense).*`)

	for _, vm := range vms {
		vnet := "net0"

		// Detect if VM is a router based on its name (lazy way but requires fewer API calls)
		if routerRegex.MatchString(vm.Name) {
			vnet = "net1"
		}

		// Update VM network configuration
		reqBody := map[string]string{
			vnet: fmt.Sprintf("virtio,bridge=%s,firewall=1", vnetName),
		}

		req := tools.ProxmoxAPIRequest{
			Method:      "PUT",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/config", vm.NodeName, vm.VmId),
			RequestBody: reqBody,
		}

		_, err := cs.ProxmoxService.GetRequestHelper().MakeRequest(req)
		if err != nil {
			return fmt.Errorf("failed to update network for VM %d: %w", vm.VmId, err)
		}
	}

	return nil
}

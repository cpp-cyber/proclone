package cloning

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/cpp-cyber/proclone/internal/tools"
)

func (cs *CloningService) getRouterType(router proxmox.VM) (string, error) {
	infoReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", router.Node, router.VMID),
	}

	infoRsp, err := cs.ProxmoxService.GetRequestHelper().MakeRequest(infoReq)
	if err != nil {
		return "", fmt.Errorf("request for router type failed: %v", err)
	}
	switch {
	case strings.Contains(string(infoRsp), "pfsense"):
		return "pfsense", nil
	case strings.Contains(string(infoRsp), "vyos"):
		return "vyos", nil
	default:
		return "", fmt.Errorf("router type not defined")
	}
}

// configurePodRouter configures the pod router with proper networking settings
func (cs *CloningService) configurePodRouter(podNumber int, node string, vmid int, routerType string) error {
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

	// Clone depending on router type
	switch routerType {
	case "pfsense":
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
	case "vyos":
		reqBody := map[string]any{
			"command": []string{
				"sh",
				"-c",
				fmt.Sprintf("sed -i -e 's/{{THIRD_OCTET}}/%d/g;s/{{NETWORK_PREFIX}}/%s/g' %s", podNumber, cs.Config.WANIPBase, cs.Config.VYOSScriptPath),
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

	default:
		return fmt.Errorf("router type invalid")
	}

	return nil
}

func (cs *CloningService) SetPodVnet(poolName string, vnetName string) error {
	// Get all VMs in the pool
	vms, err := cs.ProxmoxService.GetPoolVMs(poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs for pool %s: %w", poolName, err)
	}

	if len(vms) == 0 {
		return fmt.Errorf("pool %s contains no VMs", poolName)
	}

	log.Printf("Setting VNet %s for %d VMs in pool %s", vnetName, len(vms), poolName)

	routerRegex := regexp.MustCompile(`(?i).*(router|pfsense|vyos).*`)
	var errors []string

	for _, vm := range vms {
		vnet := "net0"

		// Detect if VM is a router based on its name (lazy way but requires fewer API calls)
		if routerRegex.MatchString(vm.Name) {
			vnet = "net1"
			log.Printf("Detected router VM %s (VMID: %d), using %s interface", vm.Name, vm.VmId, vnet)
		} else {
			log.Printf("Setting VNet for VM %s (VMID: %d), using %s interface", vm.Name, vm.VmId, vnet)
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
			errorMsg := fmt.Sprintf("failed to update network for VM %s (VMID: %d): %v", vm.Name, vm.VmId, err)
			log.Printf("ERROR: %s", errorMsg)
			errors = append(errors, errorMsg)
		} else {
			log.Printf("Successfully updated VNet for VM %s (VMID: %d) to %s", vm.Name, vm.VmId, vnetName)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("VNet configuration completed with errors: %v", errors)
	}

	log.Printf("Successfully configured VNet %s for all %d VMs in pool %s", vnetName, len(vms), poolName)
	return nil
}

package proxmox

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// RouterConfig holds configuration needed for router operations
type RouterConfig struct {
	RouterWaitTimeout time.Duration
	WANScriptPath     string
	VIPScriptPath     string
	VYOSScriptPath    string
	WANIPBase         string
}

func (s *ProxmoxService) GetRouterType(router VM) (string, error) {
	infoReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", router.Node, router.VMID),
	}

	infoRsp, err := s.RequestHelper.MakeRequest(infoReq)
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

// agentExecWithRetry retries an agent/exec request with exponential backoff.
// The QEMU guest agent may respond to pings before it's fully ready to handle
// exec commands (Proxmox returns status 596 in that case).
func (s *ProxmoxService) agentExecWithRetry(execReq tools.ProxmoxAPIRequest) error {
	backoff := 2 * time.Second
	maxBackoff := 15 * time.Second
	maxRetries := 10

	var lastErr error
	for attempt := range maxRetries {
		_, lastErr = s.RequestHelper.MakeRequest(execReq)
		if lastErr == nil {
			return nil
		}

		if !strings.Contains(lastErr.Error(), "status 596") {
			return lastErr // Non-retryable error
		}

		log.Printf("agent/exec attempt %d/%d failed with status 596, retrying in %v", attempt+1, maxRetries, backoff)
		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	return lastErr
}

// ConfigurePodRouter configures the pod router with proper networking settings
func (s *ProxmoxService) ConfigurePodRouter(podNumber int, node string, vmid int, routerType string, poolName string) error {
	config := RouterConfig{
		WANScriptPath:  s.Config.WANScriptPath,
		VIPScriptPath:  s.Config.VIPScriptPath,
		VYOSScriptPath: s.Config.VYOSScriptPath,
		WANIPBase:      s.Config.WANIPBase,
	}

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

		if _, err := s.RequestHelper.MakeRequest(statusReq); err == nil {
			break // Agent is responding
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// Configure depending on router type
	switch routerType {
	case "pfsense":
		// Configure router WAN IP to have correct third octet using qemu agent API call
		reqBody := map[string]any{
			"command": []string{
				config.WANScriptPath,
				fmt.Sprintf("%s%d.1", config.WANIPBase, podNumber),
			},
		}

		execReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: reqBody,
		}

		if err := s.agentExecWithRetry(execReq); err != nil {
			return fmt.Errorf("failed to make IP change request: %v", err)
		}

		// Send agent exec request to change VIP subnet
		vipReqBody := map[string]any{
			"command": []string{
				config.VIPScriptPath,
				fmt.Sprintf("%s%d.0", config.WANIPBase, podNumber),
			},
		}

		vipExecReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: vipReqBody,
		}

		if err := s.agentExecWithRetry(vipExecReq); err != nil {
			return fmt.Errorf("failed to make VIP change request: %v", err)
		}
	case "vyos":
		reqBody := map[string]any{
			"command": []string{
				config.VYOSScriptPath,
				fmt.Sprintf("%d", podNumber),
				config.WANIPBase,
				poolName,
			},
		}

		execReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: reqBody,
		}

		if err := s.agentExecWithRetry(execReq); err != nil {
			return fmt.Errorf("failed to run VyOS setup script: %v", err)
		}

	default:
		return fmt.Errorf("router type invalid")
	}

	return nil
}

func (s *ProxmoxService) SetPodVnet(poolName string, vnetName string, routerVMID int) error {
	// Get all VMs in the pool
	vms, err := s.GetPoolVMs(poolName)
	if err != nil {
		return fmt.Errorf("failed to get pool VMs for pool %s: %w", poolName, err)
	}

	if len(vms) == 0 {
		return fmt.Errorf("pool %s contains no VMs", poolName)
	}

	log.Printf("Setting VNet %s for %d VMs in pool %s", vnetName, len(vms), poolName)

	var errors []string

	for _, vm := range vms {
		vnet := "net0"

		// Identify the router by its VMID
		if vm.VmId == routerVMID {
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

		_, err := s.RequestHelper.MakeRequest(req)
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

func (s *ProxmoxService) GetUsedVNets() ([]VNet, error) {
	vnets := []VNet{}

	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/cluster/sdn/vnets",
	}

	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &vnets); err != nil {
		return nil, fmt.Errorf("failed to get vnets: %w", err)
	}

	return vnets, nil
}

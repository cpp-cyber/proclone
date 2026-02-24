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
	log.Printf("[kayhon] GetRouterType: Checking router type for VM Name=%s, VMID=%d, Node=%s", router.Name, router.VMID, router.Node)

	infoReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/config", router.Node, router.VMID),
	}

	infoRsp, err := s.RequestHelper.MakeRequest(infoReq)
	if err != nil {
		log.Printf("[kayhon] GetRouterType FAILED for VMID=%d: %v", router.VMID, err)
		return "", fmt.Errorf("request for router type failed: %v", err)
	}
	switch {
	case strings.Contains(string(infoRsp), "pfsense"):
		log.Printf("[kayhon] GetRouterType: VMID=%d detected as pfsense", router.VMID)
		return "pfsense", nil
	case strings.Contains(string(infoRsp), "vyos"):
		log.Printf("[kayhon] GetRouterType: VMID=%d detected as vyos", router.VMID)
		return "vyos", nil
	default:
		log.Printf("[kayhon] GetRouterType: VMID=%d router type not defined in config response", router.VMID)
		return "", fmt.Errorf("router type not defined")
	}
}

// ConfigurePodRouter configures the pod router with proper networking settings
func (s *ProxmoxService) ConfigurePodRouter(podNumber int, node string, vmid int, routerType string) error {
	log.Printf("[kayhon] ConfigurePodRouter START: PodNumber=%d, Node=%s, VMID=%d, RouterType=%s", podNumber, node, vmid, routerType)

	config := RouterConfig{
		WANScriptPath:  s.Config.WANScriptPath,
		VIPScriptPath:  s.Config.VIPScriptPath,
		VYOSScriptPath: s.Config.VYOSScriptPath,
		WANIPBase:      s.Config.WANIPBase,
	}
	log.Printf("[kayhon] ConfigurePodRouter config: WANScriptPath=%s, VIPScriptPath=%s, VYOSScriptPath=%s, WANIPBase=%s",
		config.WANScriptPath, config.VIPScriptPath, config.VYOSScriptPath, config.WANIPBase)

	// Wait for router agent to be pingable
	statusReq := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/agent/ping", node, vmid),
	}

	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	log.Printf("[kayhon] ConfigurePodRouter: Waiting for QEMU agent to respond on VMID=%d", vmid)
	for {
		if time.Since(startTime) > timeout {
			log.Printf("[kayhon] ConfigurePodRouter: QEMU agent timed out for VMID=%d after %s", vmid, timeout)
			return fmt.Errorf("router qemu agent timed out")
		}

		if _, err := s.RequestHelper.MakeRequest(statusReq); err == nil {
			log.Printf("[kayhon] ConfigurePodRouter: QEMU agent responding on VMID=%d (elapsed: %s)", vmid, time.Since(startTime))
			break // Agent is responding
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// Clone depending on router type
	log.Printf("[kayhon] ConfigurePodRouter: Applying configuration for routerType=%s on VMID=%d", routerType, vmid)
	switch routerType {
	case "pfsense":
		wanIP := fmt.Sprintf("%s%d.1", config.WANIPBase, podNumber)
		log.Printf("[kayhon] ConfigurePodRouter (pfsense): Setting WAN IP=%s on VMID=%d", wanIP, vmid)

		// Configure router WAN IP to have correct third octet using qemu agent API call
		reqBody := map[string]any{
			"command": []string{
				config.WANScriptPath,
				wanIP,
			},
		}

		execReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: reqBody,
		}

		_, err := s.RequestHelper.MakeRequest(execReq)
		if err != nil {
			log.Printf("[kayhon] ConfigurePodRouter (pfsense): WAN IP change FAILED for VMID=%d: %v", vmid, err)
			return fmt.Errorf("failed to make IP change request: %v", err)
		}
		log.Printf("[kayhon] ConfigurePodRouter (pfsense): WAN IP set successfully on VMID=%d", vmid)

		// Send agent exec request to change VIP subnet
		vipSubnet := fmt.Sprintf("%s%d.0", config.WANIPBase, podNumber)
		log.Printf("[kayhon] ConfigurePodRouter (pfsense): Setting VIP subnet=%s on VMID=%d", vipSubnet, vmid)

		vipReqBody := map[string]any{
			"command": []string{
				config.VIPScriptPath,
				vipSubnet,
			},
		}

		vipExecReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: vipReqBody,
		}

		_, err = s.RequestHelper.MakeRequest(vipExecReq)
		if err != nil {
			log.Printf("[kayhon] ConfigurePodRouter (pfsense): VIP change FAILED for VMID=%d: %v", vmid, err)
			return fmt.Errorf("failed to make VIP change request: %v", err)
		}
		log.Printf("[kayhon] ConfigurePodRouter (pfsense): VIP subnet set successfully on VMID=%d", vmid)

	case "vyos":
		log.Printf("[kayhon] ConfigurePodRouter (vyos): Substituting THIRD_OCTET=%d, NETWORK_PREFIX=%s in %s on VMID=%d",
			podNumber, config.WANIPBase, config.VYOSScriptPath, vmid)

		reqBody := map[string]any{
			"command": []string{
				"sh",
				"-c",
				fmt.Sprintf("sed -i -e 's/{{THIRD_OCTET}}/%d/g;s/{{NETWORK_PREFIX}}/%s/g' %s", podNumber, config.WANIPBase, config.VYOSScriptPath),
			},
		}

		execReq := tools.ProxmoxAPIRequest{
			Method:      "POST",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
			RequestBody: reqBody,
		}

		_, err := s.RequestHelper.MakeRequest(execReq)
		if err != nil {
			log.Printf("[kayhon] ConfigurePodRouter (vyos): Config substitution FAILED for VMID=%d: %v", vmid, err)
			return fmt.Errorf("failed to make IP change request: %v", err)
		}
		log.Printf("[kayhon] ConfigurePodRouter (vyos): Config substitution successful on VMID=%d", vmid)

	default:
		log.Printf("[kayhon] ConfigurePodRouter: Invalid router type '%s' for VMID=%d", routerType, vmid)
		return fmt.Errorf("router type invalid")
	}

	log.Printf("[kayhon] ConfigurePodRouter COMPLETE: PodNumber=%d, VMID=%d, RouterType=%s", podNumber, vmid, routerType)
	return nil
}

func (s *ProxmoxService) SetPodVnet(poolName string, vnetName string, routerVMID int) error {
	log.Printf("[kayhon] SetPodVnet START: Pool=%s, VNet=%s, RouterVMID=%d", poolName, vnetName, routerVMID)

	// Get all VMs in the pool
	vms, err := s.GetPoolVMs(poolName)
	if err != nil {
		log.Printf("[kayhon] SetPodVnet: Failed to get pool VMs for pool %s: %v", poolName, err)
		return fmt.Errorf("failed to get pool VMs for pool %s: %w", poolName, err)
	}

	if len(vms) == 0 {
		log.Printf("[kayhon] SetPodVnet: Pool %s contains no VMs", poolName)
		return fmt.Errorf("pool %s contains no VMs", poolName)
	}

	log.Printf("[kayhon] SetPodVnet: Setting VNet %s for %d VMs in pool %s", vnetName, len(vms), poolName)

	var errors []string

	for _, vm := range vms {
		vnet := "net0"

		// Identify the router by its VMID
		if vm.VmId == routerVMID {
			vnet = "net1"
			log.Printf("[kayhon] SetPodVnet: Router VM (VMID=%d, Node=%s), using %s interface", vm.VmId, vm.NodeName, vnet)
		} else {
			log.Printf("[kayhon] SetPodVnet: Regular VM (VMID=%d, Node=%s), using %s interface", vm.VmId, vm.NodeName, vnet)
		}

		// Update VM network configuration
		netConfig := fmt.Sprintf("virtio,bridge=%s,firewall=1", vnetName)
		log.Printf("[kayhon] SetPodVnet: Updating VM config VMID=%d: %s=%s", vm.VmId, vnet, netConfig)

		reqBody := map[string]string{
			vnet: netConfig,
		}

		req := tools.ProxmoxAPIRequest{
			Method:      "PUT",
			Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/config", vm.NodeName, vm.VmId),
			RequestBody: reqBody,
		}

		_, err := s.RequestHelper.MakeRequest(req)
		if err != nil {
			errorMsg := fmt.Sprintf("failed to update network for VM %s (VMID: %d): %v", vm.Name, vm.VmId, err)
			log.Printf("[kayhon] SetPodVnet ERROR: %s", errorMsg)
			errors = append(errors, errorMsg)
		} else {
			log.Printf("[kayhon] SetPodVnet: Successfully updated VM config for VMID=%d: %s=%s", vm.VmId, vnet, netConfig)
		}
	}

	if len(errors) > 0 {
		log.Printf("[kayhon] SetPodVnet COMPLETED WITH ERRORS: Pool=%s, VNet=%s, ErrorCount=%d", poolName, vnetName, len(errors))
		return fmt.Errorf("VNet configuration completed with errors: %v", errors)
	}

	log.Printf("[kayhon] SetPodVnet COMPLETE: Successfully configured VNet %s for all %d VMs in pool %s", vnetName, len(vms), poolName)
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

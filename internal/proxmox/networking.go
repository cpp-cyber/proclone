package proxmox

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// agentExecResult holds the response from agent/exec
type agentExecResult struct {
	PID int `json:"pid"`
}

// agentExecStatus holds the response from agent/exec-status
type agentExecStatus struct {
	Exited   bool   `json:"exited"`
	ExitCode int    `json:"exitcode"`
	OutData  string `json:"out-data"`
	ErrData  string `json:"err-data"`
}

// execAgentCommand runs a command via the QEMU guest agent and waits for it to complete,
// returning an error if the command fails.
func (s *ProxmoxService) execAgentCommand(node string, vmid int, command []string) error {
	reqBody := map[string]any{
		"command": command,
	}

	execReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
		RequestBody: reqBody,
	}

	rsp, err := s.RequestHelper.MakeRequest(execReq)
	if err != nil {
		return fmt.Errorf("agent exec request failed: %v", err)
	}

	var result agentExecResult
	if err := json.Unmarshal(rsp, &result); err != nil {
		return fmt.Errorf("failed to parse agent exec response: %v", err)
	}

	log.Printf("Agent exec started on VM %d with PID %d: %v", vmid, result.PID, command)

	// Poll exec-status until the command finishes
	statusReq := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec-status?pid=%d", node, vmid, result.PID),
	}

	timeout := 60 * time.Second
	startTime := time.Now()
	for {
		if time.Since(startTime) > timeout {
			return fmt.Errorf("agent exec timed out waiting for PID %d on VM %d", result.PID, vmid)
		}

		statusRsp, err := s.RequestHelper.MakeRequest(statusReq)
		if err != nil {
			// exec-status returns 500 while the command is still running
			time.Sleep(2 * time.Second)
			continue
		}

		var status agentExecStatus
		if err := json.Unmarshal(statusRsp, &status); err != nil {
			return fmt.Errorf("failed to parse agent exec-status response: %v", err)
		}

		if !status.Exited {
			time.Sleep(2 * time.Second)
			continue
		}

		if status.ExitCode != 0 {
			return fmt.Errorf("agent exec on VM %d exited with code %d, stderr: %s, stdout: %s",
				vmid, status.ExitCode, status.ErrData, status.OutData)
		}

		log.Printf("Agent exec on VM %d PID %d completed successfully", vmid, result.PID)
		return nil
	}
}

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

// ConfigurePodRouter configures the pod router with proper networking settings
func (s *ProxmoxService) ConfigurePodRouter(podNumber int, node string, vmid int, routerType string, hostname string) error {
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
		err := s.execAgentCommand(node, vmid, []string{
			config.WANScriptPath,
			fmt.Sprintf("%s%d.1", config.WANIPBase, podNumber),
		})
		if err != nil {
			return fmt.Errorf("failed to make IP change request: %v", err)
		}

		// Change VIP subnet
		err = s.execAgentCommand(node, vmid, []string{
			config.VIPScriptPath,
			fmt.Sprintf("%s%d.0", config.WANIPBase, podNumber),
		})
		if err != nil {
			return fmt.Errorf("failed to make VIP change request: %v", err)
		}
	case "vyos":
		err := s.execAgentCommand(node, vmid, []string{
			"sh",
			"-c",
			fmt.Sprintf("sed -i -e 's/{{THIRD_OCTET}}/%d/g;s/{{NETWORK_PREFIX}}/%s/g;s/{{HOSTNAME}}/%s/g' %s", podNumber, config.WANIPBase, hostname, config.VYOSScriptPath),
		})
		if err != nil {
			return fmt.Errorf("failed to make IP change request: %v", err)
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

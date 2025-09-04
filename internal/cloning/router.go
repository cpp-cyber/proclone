package cloning

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// configurePodRouter configures the pod router with proper networking settings
func (cm *CloningManager) configurePodRouter(podNumber int, node string, vmid int) error {
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

		_, err := cm.ProxmoxService.GetRequestHelper().MakeRequest(statusReq)
		if err == nil {
			break // Agent is responding
		}

		log.Printf("Agent ping failed for VMID %d", vmid)
		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// Configure router WAN IP to have correct third octet using qemu agent API call
	reqBody := map[string]any{
		"command": []string{
			cm.Config.WANScriptPath,
			fmt.Sprintf("%s%d.1", cm.Config.WANIPBase, podNumber),
		},
	}

	execReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
		RequestBody: reqBody,
	}

	_, err := cm.ProxmoxService.GetRequestHelper().MakeRequest(execReq)
	if err != nil {
		return fmt.Errorf("failed to make IP change request: %v", err)
	}

	// Send agent exec request to change VIP subnet
	vipReqBody := map[string]any{
		"command": []string{
			cm.Config.VIPScriptPath,
			fmt.Sprintf("%s%d.0", cm.Config.WANIPBase, podNumber),
		},
	}

	vipExecReq := tools.ProxmoxAPIRequest{
		Method:      "POST",
		Endpoint:    fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid),
		RequestBody: vipReqBody,
	}

	_, err = cm.ProxmoxService.GetRequestHelper().MakeRequest(vipExecReq)
	if err != nil {
		return fmt.Errorf("failed to make VIP change request: %v", err)
	}

	log.Printf("Successfully configured router for pod %d on node %s, VMID %d", podNumber, node, vmid)
	return nil
}

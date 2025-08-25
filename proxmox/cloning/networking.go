package cloning

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/cpp-cyber/proclone/proxmox"
)

type VNetResponse struct {
	VnetArray []VNet `json:"data"`
}

type VNet struct {
	Type      string `json:"type"`
	Name      string `json:"vnet"`
	Tag       int    `json:"tag,omitempty"`
	Alias     string `json:"alias,omitempty"`
	Zone      string `json:"zone"`
	VlanAware int    `json:"vlanaware,omitempty"`
}

type Config struct {
	HardDisk string `json:"scsi0"`
	Net0     string `json:"net0"`
	Net1     string `json:"net1,omitempty"`
}

type ConfigResponse struct {
	Data    Config `json:"data"`
	Success int    `json:"success"`
}

const POD_VLAN_BASE int = 1800
const SDN_ZONE string = "MainZone"
const WAN_SCRIPT_PATH string = "/home/update-wan-ip.sh"
const VIP_SCRIPT_PATH string = "/home/update-wan-vip.sh"
const WAN_IP_BASE string = "172.16."

/*
 * ----- SETS THE WAN IP ADDRESS OF A POD ROUTER -----
 * depends on the pfSense router template having a qemu agent installed and enabled
 */
func configurePodRouter(config *proxmox.ProxmoxConfig, podNum int, node string, vmid int) error {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// wait for router agent to be pingable

	statusPath := fmt.Sprintf("api2/json/nodes/%s/qemu/%d/agent/ping", node, vmid)

	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			return fmt.Errorf("router qemu agent timed out")
		}

		statusCode, _, err := proxmox.MakeRequest(config, statusPath, "POST", nil, client)
		if err != nil {
			return fmt.Errorf("")
		}

		if statusCode == http.StatusOK {
			break
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// configure router WAN ip to have correct third octet using qemu agent api call

	execPath := fmt.Sprintf("api2/json/nodes/%s/qemu/%d/agent/exec", node, vmid)

	// define json data holding new WAN IP
	reqBody := map[string]interface{}{
		"command": []string{
			WAN_SCRIPT_PATH,
			fmt.Sprintf("%s%d.1", WAN_IP_BASE, podNum),
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create ip request body: %v", err)
	}

	statusCode, body, err := proxmox.MakeRequest(config, execPath, "POST", jsonBody, client)
	if err != nil {
		return fmt.Errorf("failed to make IP change request: %v", err)
	}

	// handle response and return
	if statusCode != http.StatusOK {
		return fmt.Errorf("qemu agent failed to execute ip change script on router: %s", string(body))
	}

	// SEND AGENT EXEC REQUEST TO CHANGE VIP SUBNET

	// define json data holding new VIP subnet
	reqBody = map[string]interface{}{
		"command": []string{
			VIP_SCRIPT_PATH,
			fmt.Sprintf("%s%d.0", WAN_IP_BASE, podNum),
		},
	}

	jsonBody, err = json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create vip request body: %v", err)
	}

	statusCode, body, err = proxmox.MakeRequest(config, execPath, "POST", jsonBody, client)
	if err != nil {
		return fmt.Errorf("failed to make VIP change request: %v", err)
	}

	// handle response and return
	if statusCode != http.StatusOK {
		return fmt.Errorf("qemu agent failed to execute vip change script on router: %s", string(body))
	}

	return nil
}

/*
 * ----- CHECK BY NAME FOR VNET ALREADY IN CLUSTER -----
 */
func checkForVnet(config *proxmox.ProxmoxConfig, podID int) (exists bool, err error) {
	vnetPath := "api2/json/cluster/sdn/vnets"

	_, body, err := proxmox.MakeRequest(config, vnetPath, "GET", nil, nil)
	if err != nil {
		return false, fmt.Errorf("failed to request vnets: %v", err)
	}

	// Parse response into VMResponse struct
	var apiResp VNetResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return false, fmt.Errorf("failed to parse vnet response: %v", err)
	}

	// iterate through list of vnets and compare with desired vnet name
	vnetName := fmt.Sprintf("kamino%d", podID)

	for _, vnet := range apiResp.VnetArray {
		if vnet.Name == vnetName {
			return true, nil
		}
	}

	return false, nil
}

/*
 * ----- CREATE NEW VNET OBJECT IN THE CLUSTER SDN -----
 * SDN must be refreshed for new vnet to be used by pods
 */
func addVNetObject(config *proxmox.ProxmoxConfig, podID int) (vnet string, err error) {

	// Prepare VNet URL
	vnetPath := "api2/json/cluster/sdn/vnets"

	podVlan := POD_VLAN_BASE + podID

	// define json data holding new VNet parameters
	reqBody := map[string]interface{}{
		"vnet":      fmt.Sprintf("kamino%d", podID),
		"zone":      SDN_ZONE,
		"alias":     fmt.Sprintf("%d_pod-vnet", podVlan),
		"tag":       podVlan,
		"vlanaware": true,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request body: %v", err)
	}

	statusCode, body, err := proxmox.MakeRequest(config, vnetPath, "POST", jsonBody, nil)
	if err != nil {
		return "", fmt.Errorf("vnet create request failed: %v", err)
	}

	// handle response and return
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("failed to create new vnet object: %s", string(body))
	} else {
		return fmt.Sprintf("kamino%d", podID), nil
	}
}

/*
 * ----- APPLIES SDN CHANGES -----
 * should be called after adding or removing vnet objects
 */
func applySDNChanges(config *proxmox.ProxmoxConfig) error {
	sdnPath := "api2/json/cluster/sdn"

	statusCode, body, err := proxmox.MakeRequest(config, sdnPath, "PUT", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to apply sdn changes: %v", err)
	}

	// return based on response
	if statusCode != http.StatusOK {
		return fmt.Errorf("failed to apply changes to sdn: %s", string(body))
	} else {
		return nil
	}
}

/*
 * ----- CONFIGURES NETWORK BRIDGE (VNET) FOR ALL VMS IN A POD -----
 */
func setPodVnet(config *proxmox.ProxmoxConfig, podName string, vnet string) error {

	// Prepare VNet URL
	poolPath := fmt.Sprintf("api2/json/pools/%s", podName)

	_, body, err := proxmox.MakeRequest(config, poolPath, "GET", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to get pod pool: %v", err)
	}

	var apiResp proxmox.PoolResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("failed to parse pool response: %v", err)
	}

	for _, vm := range apiResp.Data.Members {
		err = updateVNet(config, &vm, vnet)
		if err != nil {
			return fmt.Errorf("failed to update VNet: %v", err)
		}
	}

	return nil
}

// Gets config of a specific vm
func getVMConfig(config *proxmox.ProxmoxConfig, node string, vmid int) (response *ConfigResponse, err error) {

	// Prepare config URL
	configPath := fmt.Sprintf("api2/extjs/nodes/%s/qemu/%d/config", node, vmid)

	_, body, err := proxmox.MakeRequest(config, configPath, "GET", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get vm config: %v", err)
	}

	// Parse response body
	var apiResp ConfigResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse vm config response: %v", err)
	}

	return &apiResp, nil
}

/*
 * ----- CONFIGURE NETWORK BRIDGE FOR A SINGLE VM -----
 * automatically handles configuration of normal vms and routers
 */
func updateVNet(config *proxmox.ProxmoxConfig, vm *proxmox.VirtualResource, newBridge string) error {
	// ----- get current network config -----

	apiResp, err := getVMConfig(config, vm.NodeName, vm.VmId)
	if err != nil {
		return err
	}

	// Handle vms with two interfaces (routers) seperately from vms with one interface
	if apiResp.Data.Net1 == "" {
		newConfig := replaceBridge(apiResp.Data.Net0, newBridge)
		err := setNetworkBridge(config, vm, "net0", newConfig)
		if err != nil {
			return err
		}
	} else {
		newConfig := replaceBridge(apiResp.Data.Net1, newBridge)
		err := setNetworkBridge(config, vm, "net1", newConfig)
		if err != nil {
			return err
		}
	}

	return nil
}

// helper function to replace the network bridge in a vm config using regex
func replaceBridge(netStr string, newBridge string) string {
	re := regexp.MustCompile(`bridge=[^,]+`)
	return re.ReplaceAllString(netStr, "bridge="+newBridge)
}

/*
 * ----- SET NETWORK BRIDGE FOR A SINGLE VM -----
 * automatically handles configuration of normal vms and routers
 */
func setNetworkBridge(config *proxmox.ProxmoxConfig, vm *proxmox.VirtualResource, net string, newConfig string) error {
	// ----- set network config -----
	configPath := fmt.Sprintf("api2/extjs/nodes/%s/qemu/%d/config", vm.NodeName, vm.VmId)

	// define json data holding new VNet parameters
	reqBody := map[string]interface{}{
		net: newConfig,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request body: %v", err)
	}

	statusCode, body, err := proxmox.MakeRequest(config, configPath, "PUT", jsonBody, nil)
	if err != nil {
		return fmt.Errorf("failed to set network bridge in vm config: %v", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("failed to set vm config: %s", string(body))
	}

	return nil
}

package cloning

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/P-E-D-L/proclone/proxmox"
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
	Net0 string `json:"net0"`
	Net1 string `json:"net1,omitempty"`
}

type ConfigResponse struct {
	Data    Config `json:"data"`
	Success int    `json:"success"`
}

const POD_VLAN_BASE int = 1800
const SDN_ZONE string = "MainZone"
const WAN_SCRIPT_PATH string = "/home/update-wan-ip.sh"
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

	statusURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/agent/ping",
		config.Host, config.Port, node, vmid)

	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			return fmt.Errorf("router qemu agent timed out")
		}

		req, err := http.NewRequest("POST", statusURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create agent ping request: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to check agent status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			break
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}

	// configure router WAN ip to have correct third octet using qemu agent api call

	execURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/agent/exec", config.Host,
		config.Port, node, vmid)

	// define json data holding new VNet parameters
	reqBody := map[string]interface{}{
		"command": []string{
			WAN_SCRIPT_PATH,
			fmt.Sprintf("%s%d.1", WAN_IP_BASE, podNum),
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request body: %v", err)
	}

	// create request
	req, err := http.NewRequest("POST", execURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create agent exec request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))
	req.Header.Set("Content-Type", "application/json")

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("qemu agent failed to execute IP change script on router: %v", err)
	}
	defer resp.Body.Close()

	// handle response and return
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qemu agent failed to execute IP change script on router: %s", string(body))
	}

	return nil
}

/*
 * ----- CHECK BY NAME FOR VNET ALREADY IN CLUSTER -----
 */
func checkForVnet(config *proxmox.ProxmoxConfig, podID int) (exists bool, err error) {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare VNet URL
	vnetURL := fmt.Sprintf("https://%s:%s/api2/json/cluster/sdn/vnets", config.Host, config.Port)

	// create request
	req, err := http.NewRequest("GET", vnetURL, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create vnets request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to get vnet objects: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read proxmox vnet response: %v", err)
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

	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare VNet URL
	vnetURL := fmt.Sprintf("https://%s:%s/api2/json/cluster/sdn/vnets", config.Host, config.Port)

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

	// create request
	req, err := http.NewRequest("POST", vnetURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create new vnet request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))
	req.Header.Set("Content-Type", "application/json")

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to create new vnet object: %v", err)
	}
	defer resp.Body.Close()

	// handle response and return
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
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
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare VNet URL
	sdnURL := fmt.Sprintf("https://%s:%s/api2/json/cluster/sdn", config.Host, config.Port)

	// create request
	req, err := http.NewRequest("PUT", sdnURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create sdn change request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request changes to sdn: %v", err)
	}

	// handle response and return
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to apply changes to sdn: %s", string(body))
	} else {
		return nil
	}
}

/*
 * ----- CONFIGURES NETWORK BRIDGE (VNET) FOR ALL VMS IN A POD -----
 */
func setPodVnet(config *proxmox.ProxmoxConfig, podName string, vnet string) error {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare VNet URL
	poolURL := fmt.Sprintf("https://%s:%s/api2/json/pools/%s", config.Host, config.Port, podName)

	// create request
	req, err := http.NewRequest("GET", poolURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create a resource pool request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request pool members: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read proxmox resource pool response: %v", err)
	}

	var apiResp PoolResponse
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

/*
 * ----- CONFIGURE NETWORK BRIDGE FOR A SINGLE VM -----
 * automatically handles configuration of normal vms and routers
 */
func updateVNet(config *proxmox.ProxmoxConfig, vm *proxmox.VirtualResource, newBridge string) error {
	// ----- get current network config -----

	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare config URL
	configURL := fmt.Sprintf("https://%s:%s/api2/extjs/nodes/%s/qemu/%d/config", config.Host, config.Port, vm.NodeName, vm.VmId)

	// create request
	req, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create a vm config request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to request vm config: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read proxmox vm config response: %v", err)
	}

	// Parse response body
	var apiResp ConfigResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("failed to parse vm config response: %v", err)
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
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare config URL
	configURL := fmt.Sprintf("https://%s:%s/api2/extjs/nodes/%s/qemu/%d/config", config.Host, config.Port, vm.NodeName, vm.VmId)

	// define json data holding new VNet parameters
	reqBody := map[string]interface{}{
		net: newConfig,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request body: %v", err)
	}

	// create request
	req, err := http.NewRequest("PUT", configURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create a vm config change request: %v", err)
	}

	// set respective request headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))
	req.Header.Set("Content-Type", "application/json")

	// send request with client
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to set vm config: %v", err)
	}
	defer resp.Body.Close()

	// handle response and return
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set vm config: %s", string(body))
	}

	return nil
}

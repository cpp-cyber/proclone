package cloning

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

const POD_VLAN_BASE int = 1800
const SDN_ZONE string = "MainZone"

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

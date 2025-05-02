package proxmox

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type VMResponse struct {
	Data []VirtualMachine `json:"data"`
}

type VirtualMachine struct {
	Type          string `json:"type,omitempty"`
	Id            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	NodeName      string `json:"node,omitempty"`
	ResourcePool  string `json:"pool,omitempty"`
	RunningStatus string `json:"status,omitempty"`
	Uptime        int    `json:"uptime,omitempty"`
	VmId          int    `json:"vmid,omitempty"`
}

type VirtualMachineResponse struct {
	VirtualMachines     []VirtualMachine `json:"virtual_machines"`
	VirtualMachineCount int              `json:"virtual_machine_count"`
	RunningCount        int              `json:"running_count"`
}

type VM struct {
	VMID int    `json:"vmid" binding:"required"`
	Node string `json:"node" binding:"required"`
}

type VMPower struct {
	Success int    `json:"success"`
	Data    string `json:"data"`
}

type VMPowerResponse struct {
	Success int `json:"success"`
}

func GetVirtualMachines(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is admin (redundant with middleware)
	if !isAdmin.(bool) {
		log.Printf("Unauthorized access attempt by user %s", username)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only admin users can access vm data",
		})
		return
	}

	// store proxmox config
	var config *ProxmoxConfig
	var err error
	config, err = loadProxmoxConfig()
	if err != nil {
		log.Printf("Configuration error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load Proxmox configuration: %v", err),
		})
		return
	}

	// If no proxmox host specified, return empty repsonse
	if config.Host == "" {
		log.Printf("No proxmox server configured")
		c.JSON(http.StatusOK, VirtualMachineResponse{VirtualMachines: []VirtualMachine{}})
		return
	}

	// fetch all virtual machines
	var virtualMachines *[]VirtualMachine
	var error error
	var response VirtualMachineResponse = VirtualMachineResponse{}
	response.RunningCount = 0

	// get virtual machine info and include in response
	virtualMachines, error = getVirtualMachines(config)
	response.VirtualMachines = *virtualMachines

	// get total # of virtual machines and include in response
	response.VirtualMachineCount = len(*virtualMachines)

	// get # of running virtual machines and include in response
	for _, vm := range *virtualMachines {
		if vm.RunningStatus == "running" {
			response.RunningCount++
		}
	}

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch vm list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched vm list for user %s", username)
	c.JSON(http.StatusOK, response)

}

// handles fetching all the virtual machines on the proxmox cluster
func getVirtualMachines(config *ProxmoxConfig) (*[]VirtualMachine, error) {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	vmURL := fmt.Sprintf("https://%s:%s/api2/json/cluster/resources", config.Host, config.Port)

	// Create request
	req, err := http.NewRequest("GET", vmURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add API token header
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxmox resources: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read proxmox resource response: %v", err)
	}

	// Parse response into VMResponse struct
	var apiResp VMResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %v", err)
	}

	// Extract virtual machines from response, store in VirtualMachine struct array
	var vms []VirtualMachine
	for _, r := range apiResp.Data {
		if r.Type == "qemu" {
			vms = append(vms, r)
		}
	}

	return &vms, nil

}

func PowerOffVirtualMachine(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is admin (redundant with middleware)
	if !isAdmin.(bool) {
		log.Printf("Unauthorized access attempt by user %s", username)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only admin users can access vm data",
		})
		return
	}

	// store proxmox config
	var config *ProxmoxConfig
	var err error
	config, err = loadProxmoxConfig()
	if err != nil {
		log.Printf("Configuration error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load Proxmox configuration: %v", err),
		})
		return
	}

	// If no proxmox host specified, return empty repsonse
	if config.Host == "" {
		log.Printf("No proxmox server configured")
		c.JSON(http.StatusOK, VirtualMachineResponse{VirtualMachines: []VirtualMachine{}})
		return
	}

	// If no nodes specified, return empty response
	if len(config.Nodes) == 0 {
		log.Printf("No nodes configured for user %s", username)
		c.JSON(http.StatusOK, ResourceUsageResponse{Nodes: []NodeResourceUsage{}})
		return
	}

	// get req.VMID, req.Node
	var req VM
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: must include 'vmid' and 'node'"})
		return
	}

	// log request on backend
	log.Printf("User %s requested to power off VM %d on node %s", username, req.VMID, req.Node)

	var error error
	var response *VMPower

	response, error = powerOffRequest(config, req)

	// If we have error , return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch resource usage for any nodes",
			"details": error,
		})
		return
	}

	var finalResponse VMPowerResponse
	finalResponse.Success = response.Success

	if finalResponse.Success == 1 {
		log.Printf("Successfully powered down VMID %s for %s", strconv.Itoa(req.VMID), username)
		c.JSON(http.StatusOK, response)
	} else {
		log.Printf("Failed to power down VMID %s for %s", strconv.Itoa(req.VMID), username)
		c.JSON(http.StatusOK, response)
	}

}

func powerOffRequest(config *ProxmoxConfig, vm VM) (*VMPower, error) {

	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare status URL
	statusURL := fmt.Sprintf("https://%s:%s/api2/extjs/nodes/%s/qemu/%s/status/shutdown", config.Host, config.Port, vm.Node, strconv.Itoa(vm.VMID))

	// Create request
	req, err := http.NewRequest("POST", statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add Authorization header with API token
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to shut down VM: %v", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read VM shutdown response: %v", err)
	}

	// Parse response
	var apiResp VMPower
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse VM shutdown response: %v", err)
	}

	return &apiResp, nil
}

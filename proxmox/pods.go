package proxmox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type TemplateResponse struct {
	Templates []Template `json:"templates"`
}

type Template struct {
	Name string `json:"name"`
}

type CloneRequest struct {
	TemplatePool string `json:"template_pool" binding:"required"`
	NewPodPool   string `json:"new_pod_pool" binding:"required"`
}

type CloneResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Errors  []string `json:"errors,omitempty"`
}

/*
 * ===== GET ALL CURRENT POD TEMPLATES =====
 */
func GetAvailableTemplates(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")

	// Make sure user is authenticated (redundant)
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can access pod data",
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
		c.JSON(http.StatusOK, VirtualMachineResponse{VirtualMachines: []VirtualResource{}})
		return
	}

	// fetch template reponse
	var templateResponse *TemplateResponse
	var error error

	// get Template list and assign response
	templateResponse, error = getTemplateResponse(config)

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch pod list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched pod list for user %s", username)
	c.JSON(http.StatusOK, templateResponse)
}

func getTemplateResponse(config *ProxmoxConfig) (*TemplateResponse, error) {

	// get all virtual resources from proxmox
	apiResp, err := getVirtualResources(config)

	// if error, return error
	if err != nil {
		return nil, err
	}

	// Extract pod templates from response, store in templates array
	var templateResponse TemplateResponse
	for _, r := range *apiResp {
		if r.Type == "pool" {
			reg, _ := regexp.Compile("kamino_template_.*")
			if reg.MatchString(r.ResourcePool) {
				var temp Template
				// remove kamino_template_ label when assigning the name to be returned to user
				temp.Name = r.ResourcePool[16:]
				templateResponse.Templates = append(templateResponse.Templates, temp)
			}
		}
	}

	return &templateResponse, nil
}

/*
 * ===== CLONE VMS FROM TEMPLATE POOL TO POD POOL =====
 */
func CloneTemplateToPod(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")

	// Make sure user is authenticated
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can access pod data",
		})
		return
	}

	// Parse request body
	var req CloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Load Proxmox configuration
	config, err := loadProxmoxConfig()
	if err != nil {
		log.Printf("Configuration error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load Proxmox configuration: %v", err),
		})
		return
	}

	// Get all virtual resources
	apiResp, err := getVirtualResources(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to fetch virtual resources",
			"details": err.Error(),
		})
		return
	}

	// Find VMs in template pool
	var templateVMs []VirtualResource
	for _, r := range *apiResp {
		if r.Type == "qemu" && r.ResourcePool == req.TemplatePool {
			templateVMs = append(templateVMs, r)
		}
	}

	if len(templateVMs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("No VMs found in template pool: %s", req.TemplatePool),
		})
		return
	}

	// Clone each VM
	var errors []string
	for _, vm := range templateVMs {
		err := cloneVM(config, vm, req.NewPodPool)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Failed to clone VM %s: %v", vm.Name, err))
		}
	}

	response := CloneResponse{
		Success: len(errors) == 0,
		Message: fmt.Sprintf("Cloned %d VMs from %s to %s", len(templateVMs)-len(errors), req.TemplatePool, req.NewPodPool),
		Errors:  errors,
	}

	if len(errors) > 0 {
		c.JSON(http.StatusPartialContent, response)
	} else {
		c.JSON(http.StatusOK, response)
	}
}

func cleanupFailedClone(config *ProxmoxConfig, nodeName string, vmid int) error {
	// Create HTTP client with SSL verification based on config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Prepare delete URL
	deleteURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d", 
		config.Host, config.Port, nodeName, vmid)

	// Create request
	req, err := http.NewRequest("DELETE", deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create cleanup request: %v", err)
	}

	// Add headers
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to cleanup VM: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to cleanup VM: %s", string(body))
	}

	return nil
}

func cloneVM(config *ProxmoxConfig, vm VirtualResource, newPool string) error {
	// Create a single HTTP client for all requests
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
	}
	client := &http.Client{Transport: tr}

	// Get next available VMID
	nextIDURL := fmt.Sprintf("https://%s:%s/api2/json/cluster/nextid", config.Host, config.Port)
	req, err := http.NewRequest("GET", nextIDURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create VMID request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get next VMID: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to get next VMID: %s", string(body))
	}

	var nextIDResponse struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&nextIDResponse); err != nil {
		return fmt.Errorf("failed to decode VMID response: %v", err)
	}

	newVMID, err := strconv.Atoi(nextIDResponse.Data)
	if err != nil {
		return fmt.Errorf("invalid VMID received: %v", err)
	}

	// Prepare and execute clone request
	cloneURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/clone", 
		config.Host, config.Port, vm.NodeName, vm.VmId)

	body := map[string]interface{}{
		"newid": newVMID,
		"name":  fmt.Sprintf("%s-clone", vm.Name),
		"pool":  newPool,
		"full":  1,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to create request body: %v", err)
	}

	req, err = http.NewRequest("POST", cloneURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create clone request: %v", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to clone VM: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to clone VM: %s", string(body))
	}

	// Wait for clone completion with exponential backoff
	statusURL := fmt.Sprintf("https://%s:%s/api2/json/nodes/%s/qemu/%d/status/current", 
		config.Host, config.Port, vm.NodeName, newVMID)
	
	backoff := time.Second
	maxBackoff := 30 * time.Second
	timeout := 5 * time.Minute
	startTime := time.Now()

	for {
		if time.Since(startTime) > timeout {
			if err := cleanupFailedClone(config, vm.NodeName, newVMID); err != nil {
				return fmt.Errorf("clone timed out and cleanup failed: %v", err)
			}
			return fmt.Errorf("clone operation timed out after %v", timeout)
		}

		req, err = http.NewRequest("GET", statusURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create status check request: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to check clone status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Verify the VM is actually running/ready
			var statusResponse struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&statusResponse); err != nil {
				return fmt.Errorf("failed to decode status response: %v", err)
			}
			if statusResponse.Data.Status == "running" {
				return nil // Clone completed successfully
			}
		}

		time.Sleep(backoff)
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}
}

package cloning

import (
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/P-E-D-L/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type PodResponse struct {
	Pods []Pod `json:"templates"`
}

type Pod struct {
	Name string `json:"name"`
}

func GetPods(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated (redundant)
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can see all deployed pods",
		})
		return
	}

	// store proxmox config
	var config *proxmox.ProxmoxConfig
	var err error
	config, err = proxmox.LoadProxmoxConfig()
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
		c.JSON(http.StatusOK, proxmox.VirtualMachineResponse{VirtualMachines: []proxmox.VirtualResource{}})
		return
	}

	// fetch template reponse
	var podResponse *PodResponse
	var error error

	// get Pod list and assign response
	podResponse, error = getPodResponse(config)

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch pod list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched pod list for user %s", username)
	c.JSON(http.StatusOK, podResponse)
}

func getPodResponse(config *proxmox.ProxmoxConfig) (*PodResponse, error) {

	// get all virtual resources from proxmox
	apiResp, err := proxmox.GetVirtualResources(config)

	// if error, return error
	if err != nil {
		return nil, err
	}

	// Extract pod templates from response, store in templates array
	var podResponse PodResponse
	for _, r := range *apiResp {
		if r.Type == "pool" {
			reg, _ := regexp.Compile("1[0-9][0-9][0-9]_.*")
			if reg.MatchString(r.ResourcePool) {
				var temp Pod
				// remove kamino_template_ label when assigning the name to be returned to user
				temp.Name = r.ResourcePool[16:]
				podResponse.Pods = append(podResponse.Pods, temp)
			}
		}
	}

	return &podResponse, nil
}

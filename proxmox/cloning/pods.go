package cloning

import (
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/cpp-cyber/proclone/auth"
	"github.com/cpp-cyber/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type PodResponse struct {
	Pods []PodWithVMs `json:"pods"`
}

type PodWithVMs struct {
	Name string                    `json:"name"`
	VMs  []proxmox.VirtualResource `json:"vms"`
}

// helper function that builds a maps pod names to their VMs based on the provided regex pattern
func buildPodResponse(config *proxmox.ProxmoxConfig, regexPattern string) (*PodResponse, error) {
	// get all virtual resources from proxmox
	apiResp, err := proxmox.GetVirtualResources(config)

	// if error, return error
	if err != nil {
		return nil, err
	}

	// map pod pools to their VMs
	resources := apiResp
	podMap := make(map[string]*PodWithVMs)
	reg := regexp.MustCompile(regexPattern)

	// first pass: find all pools that are pods
	for _, r := range *resources {
		if r.Type == "pool" && reg.MatchString(r.ResourcePool) {
			name := r.ResourcePool
			podMap[name] = &PodWithVMs{
				Name: name,
				VMs:  []proxmox.VirtualResource{},
			}
		}
	}

	// second pass: map VMs to their pod pool
	for _, r := range *resources {
		if r.Type == "qemu" && reg.MatchString(r.ResourcePool) {
			name := r.ResourcePool
			if pod, ok := podMap[name]; ok {
				pod.VMs = append(pod.VMs, r)
			}
		}
	}

	// build response
	var podResponse PodResponse
	for _, pod := range podMap {
		podResponse.Pods = append(podResponse.Pods, *pod)
	}

	return &podResponse, nil
}

/*
 * ===== ADMIN ENDPOINT =====
 * This function returns a list of
 * all currently deployed pods
 */
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

	// fetch pod response
	var podResponse *PodResponse
	var error error

	// get Pod list and assign response
	podResponse, error = getAdminPodResponse(config)

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch pod list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched full pod list for user %s", username)
	c.JSON(http.StatusOK, podResponse)
}

func getAdminPodResponse(config *proxmox.ProxmoxConfig) (*PodResponse, error) {
	return buildPodResponse(config, `1[0-9]{3}_.*`)
}

/*
 * ===== USER ENDPOINT =====
 * This function returns a list of
 * this user's deployed pods
 */
func GetUserPods(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")

	// Make sure user is authenticated (redundant)
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can see their deployed pods",
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
	podResponse, error = getUserPodResponse(username.(string), config)

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch user's pod list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched pod list for user %s", username)
	c.JSON(http.StatusOK, podResponse)
}

func getUserPodResponse(user string, config *proxmox.ProxmoxConfig) (*PodResponse, error) {
	return buildPodResponse(config, fmt.Sprintf(`1[0-9]{3}_.*_%s`, user))
}

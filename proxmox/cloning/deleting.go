package cloning

import (
	"fmt"
	"log"
	"net/http"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/P-E-D-L/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type DeleteRequest struct {
	PodName string `json:"pod_id"` // full pod name i.e. 1015_Some_Template_Administrator
}

type DeleteResponse = CloneResponse

/*
 * ===== DELETE CLONED VM POD =====
 */
func DeletePod(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can delete pods",
		})
		return
	}

	// Parse request body
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// Check if a non-admin user is trying to delete someone else's pod
	if !isAdmin.(bool) {
		// handle edge-case where username is longer than entire pod name
		if len(req.PodName) < len(username.(string)) {
			log.Printf("User %s attempted to delete pod %s.", username, req.PodName)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Only admin users can administer other users' pods",
			})
			return
		}
		if username.(string) != req.PodName[len(username.(string)):] {
			log.Printf("User %s attempted to delete pod %s.", username, req.PodName)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Only admin users can administer other users' pods",
			})
			return
		}
	}

	// Load Proxmox configuration
	config, err := proxmox.LoadProxmoxConfig()
	if err != nil {
		log.Printf("Configuration error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to load Proxmox configuration: %v", err),
		})
		return
	}

	// Get all virtual resources
	apiResp, err := proxmox.GetVirtualResources(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch virtual resources",
			"details": err.Error(),
		})
		return
	}

	// Check if resource pool actually exists
	var poolExists = false
	for _, r := range *apiResp {
		if r.Type == "pool" && r.ResourcePool == req.PodName {
			poolExists = true
		}
	}

	if !poolExists {
		log.Printf("User %s attempted to delete pod %s, but the resource pool doesn't exist.", username, req.PodName)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Resource pool does not exist",
		})
		return
	}

	// Find all vms in resource pool
	var podVMs []proxmox.VirtualResource
	for _, r := range *apiResp {
		if r.Type == "qemu" && r.ResourcePool == req.PodName {
			podVMs = append(podVMs, r)
		}
	}

	var errors []string

	// if no vms in pool take note, otherwise delete them all
	if len(podVMs) == 0 {
		log.Printf("No VMs found in pod pool: %s", req.PodName)
	} else {
		for _, vm := range podVMs {
			// should probably change this function name to just "cleanupClone" at this point
			err := cleanupFailedClone(config, vm.NodeName, vm.VmId)
			if err != nil {
				errors = append(errors, fmt.Sprintf("Failed to delete VM %s: %v", vm.Name, err))
			}
		}
	}

	// delete resource pool
	err = cleanupFailedPodPool(config, req.PodName)

	if err != nil {
		errors = append(errors, fmt.Sprintf("Failed to delete pod pool %s: %v", req.PodName, err))
	}

	var success int = 0
	if len(errors) == 0 {
		success = 1
	}

	response := DeleteResponse{
		Success: success,
		PodName: req.PodName,
		Errors:  errors,
	}

	if len(errors) > 0 {
		c.JSON(http.StatusPartialContent, response)
	} else {
		c.JSON(http.StatusOK, response)
	}
}

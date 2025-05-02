package proxmox

import (
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type TemplateResponse struct {
	Templates []VirtualResource `json:"templates"`
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

	// fetch all resource pools
	var virtualResources *[]VirtualResource
	var error error
	var response TemplateResponse = TemplateResponse{}

	// get Template list and assign response
	virtualResources, error = getTemplateResponse(config)
	response.Templates = *virtualResources

	// if error, return error status
	if error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch pod list from proxmox cluster",
			"details": error,
		})
		return
	}

	log.Printf("Successfully fetched pod list for user %s", username)
	c.JSON(http.StatusOK, response)
}

func getTemplateResponse(config *ProxmoxConfig) (*[]VirtualResource, error) {

	// get all virtual resources from proxmox
	apiResp, err := getVirtualResources(config)

	// if error, return error
	if err != nil {
		return nil, err
	}

	// Extract virtual machines from response, store in VirtualMachine struct array
	var templates []VirtualResource
	for _, r := range *apiResp {
		if r.Type == "pool" {
			reg, _ := regexp.Compile("kamino_template_.*")
			if reg.MatchString(r.ResourcePool) {
				templates = append(templates, r)
			}
		}
	}

	return &templates, nil
}

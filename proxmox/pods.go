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
	Templates []Template `json:"templates"`
}

type Template struct {
	Name string `json:"name"`
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

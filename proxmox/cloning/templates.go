package cloning

import (
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/P-E-D-L/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type TemplateResponse struct {
	Templates []TemplateWithVMs `json:"templates"`
}

type TemplateWithVMs struct {
	Name        string                    `json:"name"`
	Deployments int                       `json:"deployments"`
	VMs         []proxmox.VirtualResource `json:"vms"`
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
			"error": "Only authenticated users can access template data",
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

	// fetch template response
	templateResponse, err := getTemplateResponse(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch template list from proxmox cluster",
			"details": err,
		})
		return
	}

	log.Printf("Successfully fetched template list for user %s", username)
	c.JSON(http.StatusOK, templateResponse)
}

func getTemplateResponse(config *proxmox.ProxmoxConfig) (*TemplateResponse, error) {

	// get all virtual resources from proxmox
	resources, err := proxmox.GetVirtualResources(config)
	if err != nil {
		return nil, err
	}

	// map template pools to their VMs
	templateMap := make(map[string]*TemplateWithVMs)
	reg := regexp.MustCompile(`kamino_template_.*`)

	// first pass: find all pools that are templates
	for _, r := range *resources {
		if r.Type == "pool" && reg.MatchString(r.ResourcePool) {
			name := r.ResourcePool[16:]
			templateMap[name] = &TemplateWithVMs{
				Name:        name,
				Deployments: 0,
				VMs:         []proxmox.VirtualResource{},
			}
		}
	}

	// second pass: map VMs to their template pool
	for _, r := range *resources {
		if r.Type == "qemu" && reg.MatchString(r.ResourcePool) {
			name := r.ResourcePool[16:]
			if template, ok := templateMap[name]; ok {
				template.VMs = append(template.VMs, r)
			}
		}
	}

	// build response
	var templateResponse TemplateResponse
	for _, template := range templateMap {
		templateResponse.Templates = append(templateResponse.Templates, *template)
	}

	return &templateResponse, nil
}

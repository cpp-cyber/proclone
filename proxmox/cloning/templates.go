package cloning

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"slices"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/P-E-D-L/proclone/database"
	"github.com/P-E-D-L/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type ProxmoxPool struct {
	PoolID string `json:"poolid"`
}

type TemplateResponse struct {
	Templates []database.Template `json:"templates"`
}

type ProxmoxPoolResponse struct {
	Pools []ProxmoxPool `json:"pools"`
}

type UnpublishedTemplateResponse struct {
	Templates []UnpublishedTemplate `json:"templates"`
}

type UnpublishedTemplate struct {
	Name string `json:"name"`
}

/*
 * /api/proxmox/templates
 * Returns a list of templates based on their current visibility
 */
func GetAvailableTemplates(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")

	// Make sure user is authenticated
	isAuth, _ := auth.IsAuthenticated(c)
	if !isAuth {
		log.Printf("Unauthorized access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only authenticated users can access templates",
		})
		return
	}

	// fetch template response from database
	templates, err := database.SelectVisibleTemplates()
	if err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to fetch templates from database: %v", err),
		})
		return
	}

	// convert templates to response format
	templatesResponse, err := BuildTemplatesResponse(templates)
	if err != nil {
		log.Printf("Failed to get available templates response for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to process templates: %v", err),
		})
		return
	}

	log.Printf("Successfully fetched %d templates for user %s", len(templates), username)
	c.JSON(http.StatusOK, templatesResponse)
}

/*
 * /api/admin/proxmox/templates
 * Returns a list of all templates
 */
func GetAllTemplates(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can create a template",
		})
		return
	}

	// fetch template response from database
	templates, err := database.SelectAllTemplates()
	if err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to fetch templates from database: %v", err),
		})
		return
	}

	// convert templates to response format
	templatesResponse, err := BuildTemplatesResponse(templates)
	if err != nil {
		log.Printf("Failed to get available templates response for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to process templates: %v", err),
		})
		return
	}

	log.Printf("Successfully fetched %d templates for user %s", len(templates), username)
	c.JSON(http.StatusOK, templatesResponse)
}

func BuildTemplatesResponse(templates []database.Template) (TemplateResponse, error) {
	return TemplateResponse{Templates: templates}, nil
}

func GetUnpublishedTemplates(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is admin (redundant)
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can create a template",
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

	// If no proxmox host specified, return empty response
	if config.Host == "" {
		log.Printf("No proxmox server configured")
		c.JSON(http.StatusOK, UnpublishedTemplateResponse{Templates: []UnpublishedTemplate{}})
		return
	}

	// Get all Kamino templates from Proxmox
	allKaminoTemplates, err := getAllKaminoTemplateNames(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch pool list from proxmox cluster",
			"details": err,
		})
		return
	}

	// Get published template names from database
	publishedTemplateNames, err := database.SelectAllTemplateNames()
	if err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to fetch published templates from database: %v", err),
		})
		return
	}

	// Get unpublished templates (templates in Proxmox but not in database)
	unpublishedTemplates := getUnpublishedTemplateNames(allKaminoTemplates, publishedTemplateNames)

	log.Printf("Successfully fetched unpublished template list for admin user %s", username)
	c.JSON(http.StatusOK, UnpublishedTemplateResponse{Templates: unpublishedTemplates})
}

func getAllKaminoTemplateNames(config *proxmox.ProxmoxConfig) (*ProxmoxPoolResponse, error) {
	// Fetch pools from Proxmox API
	statusCode, body, err := proxmox.MakeRequest(config, "api2/json/pools", "GET", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pools from Proxmox: %v", err)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("proxmox API returned status %d", statusCode)
	}

	// Parse the response
	var apiResp proxmox.ProxmoxAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse pools response: %v", err)
	}

	// Parse the pools data
	var pools []ProxmoxPool
	if err := json.Unmarshal(apiResp.Data, &pools); err != nil {
		return nil, fmt.Errorf("failed to extract pools from response: %v", err)
	}

	// Filter pools that start with "kamino_template_"
	var templatePools []ProxmoxPool
	for _, pool := range pools {
		if strings.HasPrefix(pool.PoolID, "kamino_template_") {
			templatePools = append(templatePools, pool)
		}
	}

	return &ProxmoxPoolResponse{Pools: templatePools}, nil
}

func GetUnpublishedTemplatesResponse(allKaminoTemplates *ProxmoxPoolResponse, publishedTemplateNames []string) (*ProxmoxPoolResponse, error) {
	var unpublishedPools []ProxmoxPool

	for _, pool := range allKaminoTemplates.Pools {
		if !slices.Contains(publishedTemplateNames, pool.PoolID) {
			unpublishedPools = append(unpublishedPools, pool)
		}
	}

	return &ProxmoxPoolResponse{Pools: unpublishedPools}, nil
}

// getUnpublishedTemplateNames returns a list of template names that are in Proxmox but not published in the database
func getUnpublishedTemplateNames(allKaminoTemplates *ProxmoxPoolResponse, publishedTemplateNames []string) []UnpublishedTemplate {
	var unpublishedTemplates []UnpublishedTemplate

	for _, pool := range allKaminoTemplates.Pools {
		// Remove the "kamino_template_" prefix to get the actual template name
		templateName := strings.TrimPrefix(pool.PoolID, "kamino_template_")

		// Check if this template name is not in the published list
		if !slices.Contains(publishedTemplateNames, templateName) {
			unpublishedTemplates = append(unpublishedTemplates, UnpublishedTemplate{
				Name: templateName,
			})
		}
	}

	return unpublishedTemplates
}

/*
 * /api/admin/proxmox/templates/publish
 * This function publishes a template that is on proxmox
 */
func PublishTemplate(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated (redundant)
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can create a template",
		})
		return
	}

	var template database.Template
	if err := c.ShouldBindJSON(&template); err != nil {
		log.Printf("Failed to bind JSON for user %s: %v", username, err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request payload",
		})
		return
	}

	// Insert the new template into the database
	if err := database.InsertTemplate(template); err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to create template: %v", err),
		})
		return
	}

	log.Printf("Successfully created template %s for admin user %s", template.Name, username)
	c.JSON(http.StatusCreated, gin.H{
		"message": "Template created successfully",
	})
}

/*
 * /api/admin/proxmox/templates/update
 * This function updates an existing template
 */
func UpdateTemplate(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated and is an admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can update a template",
		})
		return
	}

	var template database.Template
	if err := c.ShouldBindJSON(&template); err != nil {
		log.Printf("Failed to bind JSON for user %s: %v", username, err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request payload",
		})
		return
	}

	// Update the template in the database
	if err := database.UpdateTemplate(template); err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to update template: %v", err),
		})
		return
	}

	log.Printf("Successfully updated template %s for admin user %s", template.Name, username)
	c.JSON(http.StatusOK, gin.H{
		"message": "Template updated successfully",
	})
}

/*
 * /api/admin/proxmox/templates/toggle
 * This function toggles the visibility of a published template
 */
func ToggleTemplateVisibility(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated and is an admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can update a template",
		})
		return
	}

	var req struct {
		TemplateName string `json:"template_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Failed to bind JSON for user %s: %v", username, err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request payload",
		})
		return
	}
	templateName := req.TemplateName

	// Update the template in the database
	if err := database.ToggleVisibility(templateName); err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to update template: %v", err),
		})
		return
	}

	log.Printf("Successfully toggled template visibility %s for admin user %s", templateName, username)
	c.JSON(http.StatusOK, gin.H{
		"message": "Template visibility toggled successfully",
	})
}

/*
 * /api/admin/proxmox/templates/delete
 * This function deletes a template
 */
func DeleteTemplate(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// Make sure user is authenticated and is an admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can delete a template",
		})
		return
	}

	var req struct {
		TemplateName string `json:"template_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Failed to bind JSON for user %s: %v", username, err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request payload",
		})
		return
	}
	templateName := req.TemplateName

	// Delete the template from the database
	if err := database.DeleteTemplate(templateName); err != nil {
		log.Printf("Database error for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("Failed to delete template: %v", err),
		})
		return
	}

	log.Printf("Successfully deleted template %s for admin user %s", templateName, username)
	c.JSON(http.StatusOK, gin.H{
		"message": "Template deleted successfully",
	})
}

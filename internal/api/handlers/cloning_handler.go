package handlers

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cpp-cyber/proclone/internal/cloning"
	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/cpp-cyber/proclone/internal/tools"
	"github.com/cpp-cyber/proclone/internal/tools/sse"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// NewCloningHandler creates a new cloning handler, loading dependencies internally
func NewCloningHandler() (*CloningHandler, error) {
	// Initialize database connection
	dbClient, err := tools.NewDBClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database client: %w", err)
	}

	// Initialize Proxmox service
	proxmoxService, err := proxmox.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create Proxmox service: %w", err)
	}

	// Initialize LDAP service
	ldapService, err := ldap.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	// Initialize Cloning manager
	cloningService, err := cloning.NewCloningService(proxmoxService, dbClient.DB(), ldapService)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cloning manager: %w", err)
	}
	log.Println("Cloning manager initialized")

	return &CloningHandler{
		Service:  cloningService,
		dbClient: dbClient,
	}, nil
}

// CloneTemplateHandler handles requests to clone a template pool for a user or group
func (ch *CloningHandler) CloneTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req CloneRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("User %s requested cloning of template %s", username, req.Template)

	publishedTemplates, err := ch.Service.DatabaseService.GetPublishedTemplates()
	if err != nil {
		log.Printf("Error fetching published templates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch published templates",
			"details": err.Error(),
		})
		return
	}

	// Check if the requested template is in the list of published templates
	templateFound := false
	for _, tmpl := range publishedTemplates {
		if tmpl.Name == req.Template {
			templateFound = true
			break
		}
	}

	if !templateFound {
		log.Printf("Template %s not found or not published", req.Template)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Template not found or not published",
			"details": fmt.Sprintf("Template %s is not available for cloning", req.Template),
		})
		return
	}

	// Create new sse object for streaming
	sseWriter, err := sse.NewWriter(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to initialize SSE",
			"details": err.Error(),
		})
		return
	}

	sseWriter.Send(
		cloning.ProgressMessage{
			Message:  "Starting cloning process...",
			Progress: 0,
		},
	)

	// Create the cloning request using the new format
	cloneReq := cloning.CloneRequest{
		Template:                 req.Template,
		CheckExistingDeployments: true, // Check for existing deployments for single user clones
		Targets: []cloning.CloneTarget{
			{
				Name:    username,
				IsGroup: false,
			},
		},
		SSE: sseWriter,
	}

	if err := ch.Service.CloneTemplate(cloneReq); err != nil {
		log.Printf("Error cloning template: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to clone template",
			"details": err.Error(),
		})
		return
	}

	log.Printf("Template %s cloned successfully for user %s", req.Template, username)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ADMIN: BulkCloneTemplateHandler handles POST requests for cloning multiple templates for a list of users
func (ch *CloningHandler) AdminCloneTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req AdminCloneRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("%s requested bulk cloning of template %s", username, req.Template)

	// Build targets slice from usernames and groups
	var targets []cloning.CloneTarget

	// Add users as targets
	for _, user := range req.Usernames {
		targets = append(targets, cloning.CloneTarget{
			Name:    user,
			IsGroup: false,
		})
	}

	// Add groups as targets
	for _, group := range req.Groups {
		targets = append(targets, cloning.CloneTarget{
			Name:    group,
			IsGroup: true,
		})
	}

	// Create new sse object for streaming
	sseWriter, err := sse.NewWriter(c.Writer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to initialize SSE",
			"details": err.Error(),
		})
		return
	}

	// Create clone request
	cloneReq := cloning.CloneRequest{
		Template:                 req.Template,
		Targets:                  targets,
		CheckExistingDeployments: false,
		StartingVMID:             req.StartingVMID,
		SSE:                      sseWriter,
	}

	// Perform clone operation
	err = ch.Service.CloneTemplate(cloneReq)
	if err != nil {
		log.Printf("Admin %s encountered error while bulk cloning template: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to clone templates",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Templates cloned successfully",
	})
}

// DeletePodHandler handles requests to delete a pod
func (ch *CloningHandler) DeletePodHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req DeletePodRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("User %s requested deletion of pod %s", username, req.Pod)

	// Check if the pod belongs to the user (maybe allow users to delete group pods in the future?)
	if !strings.Contains(req.Pod, username) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "You do not have permission to delete this pod",
			"details": fmt.Sprintf("Pod %s does not belong to user %s", req.Pod, username),
		})
		return
	}

	err := ch.Service.DeletePod(req.Pod)
	if err != nil {
		log.Printf("Error deleting %s pod: %v", req.Pod, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete pod",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pod deleted successfully"})
}

func (ch *CloningHandler) AdminDeletePodHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req AdminDeletePodRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("Admin %s requested deletion of pods: %v", username, req.Pods)

	var errors []error
	for _, pod := range req.Pods {
		err := ch.Service.DeletePod(pod)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to delete pod %s: %v", pod, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Admin %s encountered errors while deleting pods: %v", username, errors)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete pods",
			"details": errors,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pods deleted successfully"})
}

func (ch *CloningHandler) GetUnpublishedTemplatesHandler(c *gin.Context) {
	templates, err := ch.Service.GetUnpublishedTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve unpublished templates",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
		"count":     len(templates),
	})
}

// PRIVATE: GetPodsHandler handles GET requests for retrieving a user's pods
func (ch *CloningHandler) GetPodsHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	pods, err := ch.Service.GetPods(username)
	if err != nil {
		log.Printf("Error retrieving pods for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve pods", "details": err.Error()})
		return
	}

	// Loop through the user's deployed pods and add template information
	for i := range pods {
		templateName := strings.Replace(strings.ToLower(pods[i].Name[5:]), fmt.Sprintf("_%s", strings.ToLower(username)), "", 1)
		templateInfo, err := ch.Service.DatabaseService.GetTemplateInfo(templateName)
		if err != nil {
			log.Printf("Error retrieving template info for pod %s: %v", pods[i].Name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template info for pod", "details": err.Error()})
			return
		}
		pods[i].Template = templateInfo
	}

	c.JSON(http.StatusOK, gin.H{"pods": pods})
}

// ADMIN: AdminGetPodsHandler handles GET requests for retrieving all pods
func (ch *CloningHandler) AdminGetPodsHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	pods, err := ch.Service.AdminGetPods()
	if err != nil {
		log.Printf("Error retrieving all pods for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve pods for user", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pods": pods})
}

// PRIVATE: GetTemplatesHandler handles GET requests for retrieving templates
func (ch *CloningHandler) GetTemplatesHandler(c *gin.Context) {
	templates, err := ch.Service.DatabaseService.GetTemplates()
	if err != nil {
		log.Printf("Error retrieving templates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve templates",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
		"count":     len(templates),
	})
}

// ADMIN: GetPublishedTemplatesHandler handles GET requests for retrieving all templates
func (ch *CloningHandler) AdminGetTemplatesHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	templates, err := ch.Service.DatabaseService.GetPublishedTemplates()
	if err != nil {
		log.Printf("Error retrieving all templates for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve all templates",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
		"count":     len(templates),
	})
}

// PRIVATE: GetTemplateImageHandler handles GET requests for retrieving a template's image
func (ch *CloningHandler) GetTemplateImageHandler(c *gin.Context) {
	filename := c.Param("filename")
	config := ch.Service.DatabaseService.GetTemplateConfig()
	filePath := filepath.Join(config.UploadDir, filename)

	// Serve the file
	c.File(filePath)
}

// ADMIN: PublishTemplateHandler handles POST requests for publishing a template
func (ch *CloningHandler) PublishTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req PublishTemplateRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("Admin %s requested publishing of template %s", username, req.Template.Name)

	if err := ch.Service.PublishTemplate(req.Template); err != nil {
		log.Printf("Error publishing template for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to publish template",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template published successfully",
	})
}

// ADMIN: EditTemplateHandler handles POST requests for editing a published template
func (ch *CloningHandler) EditTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req PublishTemplateRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("Admin %s requested editing of template %s", username, req.Template.Name)

	if err := ch.Service.DatabaseService.EditTemplate(req.Template); err != nil {
		log.Printf("Error editing template for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to edit template",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template edited successfully",
	})
}

// ADMIN: DeleteTemplateHandler handles POST requests for deleting a template
func (ch *CloningHandler) DeleteTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req TemplateRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("Admin %s requested deletion of template %s", username, req.Template)

	if err := ch.Service.DatabaseService.DeleteTemplate(req.Template); err != nil {
		log.Printf("Error deleting template for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete template",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template deleted successfully",
	})
}

// ADMIN: ToggleTemplateVisibilityHandler handles POST requests for toggling a template's visibility
func (ch *CloningHandler) ToggleTemplateVisibilityHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req TemplateRequest
	if !validateAndBind(c, &req) {
		return
	}

	log.Printf("Admin %s requested toggling visibility of template %s", username, req.Template)

	if err := ch.Service.DatabaseService.ToggleTemplateVisibility(req.Template); err != nil {
		log.Printf("Error toggling template visibility for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to toggle template visibility",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Template visibility toggled successfully",
	})
}

// ADMIN: UploadTemplateImageHandler handles POST requests for uploading a template's image
func (ch *CloningHandler) UploadTemplateImageHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	log.Printf("Admin %s requested uploading a template image", username)

	result, err := ch.Service.DatabaseService.UploadTemplateImage(c)
	if err != nil {
		log.Printf("Error uploading template image for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to upload template image",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, result)
}

// HealthCheck checks the database connection health
func (ch *CloningHandler) HealthCheck() error {
	return ch.dbClient.HealthCheck()
}

// Reconnect attempts to reconnect to the database
func (ch *CloningHandler) Reconnect() error {
	return ch.dbClient.Connect()
}

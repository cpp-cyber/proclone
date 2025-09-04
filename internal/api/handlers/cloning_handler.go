package handlers

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cpp-cyber/proclone/internal/auth"
	"github.com/cpp-cyber/proclone/internal/cloning"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/cpp-cyber/proclone/internal/tools"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// CloningHandler holds the cloning manager
type CloningHandler struct {
	Manager  *cloning.CloningManager
	dbClient *tools.DBClient
}

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
	ldapService, err := auth.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	// Initialize Cloning manager
	cloningManager, err := cloning.NewCloningManager(proxmoxService, dbClient.DB(), ldapService)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize cloning manager: %w", err)
	}
	log.Println("Cloning manager initialized")

	return &CloningHandler{
		Manager:  cloningManager,
		dbClient: dbClient,
	}, nil
}

// CloneTemplateHandler handles requests to clone a template pool for a user or group
func (ch *CloningHandler) CloneTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var req CloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Construct the full template pool name
	templatePoolName := "kamino_template_" + req.Template

	err := ch.Manager.CloneTemplate(templatePoolName, username, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to clone template",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  fmt.Sprintf("Pod template cloned successfully for user %s", username),
		"template": req.Template,
	})
}

// ADMIN: BulkCloneTemplateHandler handles POST requests for cloning multiple templates for a list of users
func (ch *CloningHandler) AdminCloneTemplateHandler(c *gin.Context) {
	var req AdminCloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Verify that template is not blank
	if req.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "No template specified",
			"details": "A template must be specified",
		})
		return
	}

	// Verify that users and groups are not empty
	if len(req.Usernames) == 0 && len(req.Groups) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "No users or groups specified",
			"details": "At least one user or group must be specified",
		})
		return
	}

	// Construct the full template pool name
	templatePoolName := "kamino_template_" + req.Template

	// Clone for users
	var errors []error
	for _, username := range req.Usernames {
		err := ch.Manager.CloneTemplate(templatePoolName, username, false)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to clone template for user %s: %v", username, err))
		}
	}

	// Clone for groups
	for _, group := range req.Groups {
		err := ch.Manager.CloneTemplate(templatePoolName, group, true)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to clone template for group %s: %v", group, err))
		}
	}

	// Check for errors
	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to clone templates",
			"details": errors,
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
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	// Check if the pod belongs to the user
	if !strings.Contains(req.Pod, username) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "You do not have permission to delete this pod",
			"details": fmt.Sprintf("Pod %s does not belong to user %s", req.Pod, username),
		})
		return
	}

	err := ch.Manager.DeletePod(req.Pod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete pod",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pod deleted successfully"})
}

func (ch *CloningHandler) AdminDeletePodHandler(c *gin.Context) {
	var req AdminDeletePodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}

	var errors []error
	for _, pod := range req.Pods {
		err := ch.Manager.DeletePod(pod)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to delete pod %s: %v", pod, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to delete pods",
			"details": errors,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Pods deleted successfully"})
}

func (ch *CloningHandler) GetUnpublishedTemplatesHandler(c *gin.Context) {
	templates, err := ch.Manager.GetUnpublishedTemplates()
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

	pods, err := ch.Manager.GetPods(username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve pods for user " + username, "details": err.Error()})
		return
	}

	// Loop through the user's deployed pods and add template information
	for i := range pods {
		templateName := strings.Replace(pods[i].Name[5:], fmt.Sprintf("_%s", username), "", 1)
		templateInfo, err := ch.Manager.DatabaseService.GetTemplateInfo(templateName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template info for pod " + pods[i].Name, "details": err.Error()})
			return
		}
		pods[i].Template = templateInfo
	}

	c.JSON(http.StatusOK, gin.H{"pods": pods})
}

// ADMIN: GetAllPodsHandler handles GET requests for retrieving all pods
func (ch *CloningHandler) AdminGetPodsHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	pods, err := ch.Manager.GetAllPods()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve pods for user " + username, "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"pods": pods})
}

// Template-related handlers

// PRIVATE: GetTemplatesHandler handles GET requests for retrieving templates
func (ch *CloningHandler) GetTemplatesHandler(c *gin.Context) {
	templates, err := ch.Manager.DatabaseService.GetTemplates()
	if err != nil {
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
	templates, err := ch.Manager.DatabaseService.GetPublishedTemplates()
	if err != nil {
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
	config := ch.Manager.DatabaseService.GetTemplateConfig()
	filePath := filepath.Join(config.UploadDir, filename)

	// Serve the file
	c.File(filePath)
}

// ADMIN: PublishTemplateHandler handles POST requests for publishing a template
func (ch *CloningHandler) PublishTemplateHandler(c *gin.Context) {
	var req PublishTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ch.Manager.PublishTemplate(req.Template); err != nil {
		log.Printf("Error publishing template: %v", err)
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

// ADMIN: DeleteTemplateHandler handles POST requests for deleting a template
func (ch *CloningHandler) DeleteTemplateHandler(c *gin.Context) {
	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ch.Manager.DatabaseService.DeleteTemplate(req.Template); err != nil {
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
	var req TemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ch.Manager.DatabaseService.ToggleTemplateVisibility(req.Template); err != nil {
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
	result, err := ch.Manager.DatabaseService.UploadTemplateImage(c)
	if err != nil {
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

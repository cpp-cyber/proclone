package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// NewDashboardHandler creates a new dashboard handler
func NewDashboardHandler(authHandler *AuthHandler, proxmoxHandler *ProxmoxHandler, cloningHandler *CloningHandler) *DashboardHandler {
	return &DashboardHandler{
		authHandler:    authHandler,
		proxmoxHandler: proxmoxHandler,
		cloningHandler: cloningHandler,
	}
}

// ADMIN: GetDashboardStatsHandler retrieves all dashboard statistics in a single request
func (dh *DashboardHandler) GetAdminDashboardStatsHandler(c *gin.Context) {
	stats := DashboardStats{}

	// Get user count
	users, err := dh.authHandler.ldapService.GetUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user count", "details": err.Error()})
		return
	}
	stats.UserCount = len(users)

	// Get group count
	groups, err := dh.authHandler.ldapService.GetGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve group count", "details": err.Error()})
		return
	}
	stats.GroupCount = len(groups)

	// Get published template count
	publishedTemplates, err := dh.cloningHandler.Service.DatabaseService.GetPublishedTemplates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve published template count", "details": err.Error()})
		return
	}
	stats.PublishedTemplateCount = len(publishedTemplates)

	// Get deployed pod count
	pods, err := dh.cloningHandler.Service.AdminGetPods()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve deployed pod count", "details": err.Error()})
		return
	}
	stats.DeployedPodCount = len(pods)

	// Get virtual machine count
	vms, err := dh.proxmoxHandler.service.GetVMs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve virtual machine count", "details": err.Error()})
		return
	}
	stats.VirtualMachineCount = len(vms)

	// Get cluster resource usage
	clusterUsage, err := dh.proxmoxHandler.service.GetClusterResourceUsage()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve cluster resource usage", "details": err.Error()})
		return
	}
	stats.ClusterResourceUsage = clusterUsage

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
	})
}

// PRIVATE: GetUserDashboardStatsHandler retrieves all user dashboard statistics in a single request
func (dh *DashboardHandler) GetUserDashboardStatsHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	// Get user's deployed pods
	pods, err := dh.cloningHandler.Service.GetPods(username)
	if err != nil {
		log.Printf("Error retrieving pods for user %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve pods", "details": err.Error()})
		return
	}

	// Loop through the user's deployed pods and add template information
	for i := range pods {
		templateName := strings.Replace(strings.ToLower(pods[i].Name[5:]), fmt.Sprintf("_%s", strings.ToLower(username)), "", 1)
		templateInfo, err := dh.cloningHandler.Service.DatabaseService.GetTemplateInfo(templateName)
		if err != nil {
			log.Printf("Error retrieving template info for pod %s: %v", pods[i].Name, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve template info for pod", "details": err.Error()})
			return
		}
		pods[i].Template = templateInfo
	}

	// Get user's information
	userInfo, err := dh.authHandler.ldapService.GetUser(username)
	if err != nil {
		log.Printf("Error retrieving user info for %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve user info",
			"details": err.Error(),
		})
		return
	}

	// Get public pod templates
	templates, err := dh.cloningHandler.Service.DatabaseService.GetTemplates()
	if err != nil {
		log.Printf("Error retrieving templates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve templates",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"pods":      pods,
		"user_info": userInfo,
		"templates": templates,
	})
}

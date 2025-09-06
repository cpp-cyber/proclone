package handlers

import (
	"net/http"

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
func (dh *DashboardHandler) GetDashboardStatsHandler(c *gin.Context) {
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

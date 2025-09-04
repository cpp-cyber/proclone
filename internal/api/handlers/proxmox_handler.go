package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-gonic/gin"
)

// ProxmoxHandler handles HTTP requests for Proxmox operations
type ProxmoxHandler struct {
	service proxmox.Service
}

// NewProxmoxHandler creates a new Proxmox handler, loading configuration internally
func NewProxmoxHandler() (*ProxmoxHandler, error) {
	proxmoxService, err := proxmox.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create Proxmox service: %w", err)
	}

	log.Println("Proxmox handler initialized")

	return &ProxmoxHandler{
		service: proxmoxService,
	}, nil
}

// ADMIN: GetClusterResourceUsageHandler retrieves and formats the total cluster resource usage in addition to each individual node's usage
func (ph *ProxmoxHandler) GetClusterResourceUsageHandler(c *gin.Context) {
	response, err := ph.service.GetClusterResourceUsage()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve cluster resource usage", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": response,
	})
}

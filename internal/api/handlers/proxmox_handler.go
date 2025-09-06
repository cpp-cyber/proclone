package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-gonic/gin"
)

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
		log.Printf("Error retrieving cluster resource usage: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve cluster resource usage", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster": response,
	})
}

// ADMIN: GetVMsHandler handles GET requests for retrieving all VMs on Proxmox
func (ph *ProxmoxHandler) GetVMsHandler(c *gin.Context) {
	vms, err := ph.service.GetVMs()
	if err != nil {
		log.Printf("Error retrieving VMs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve VMs", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vms": vms})
}

// ADMIN: StartVMHandler handles POST requests for starting a VM on Proxmox
func (ph *ProxmoxHandler) StartVMHandler(c *gin.Context) {
	var req VMActionRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.StartVM(req.Node, req.VMID); err != nil {
		log.Printf("Error starting VM %d on node %s: %v", req.VMID, req.Node, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM started"})
}

// ADMIN: ShutdownVMHandler handles POST requests for shutting down a VM on Proxmox
func (ph *ProxmoxHandler) ShutdownVMHandler(c *gin.Context) {
	var req VMActionRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.ShutdownVM(req.Node, req.VMID); err != nil {
		log.Printf("Error shutting down VM %d on node %s: %v", req.VMID, req.Node, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to shutdown VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM shutdown"})
}

// ADMIN: RebootVMHandler handles POST requests for rebooting a VM on Proxmox
func (ph *ProxmoxHandler) RebootVMHandler(c *gin.Context) {
	var req VMActionRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.RebootVM(req.Node, req.VMID); err != nil {
		log.Printf("Error rebooting VM %d on node %s: %v", req.VMID, req.Node, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reboot VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM rebooted"})
}

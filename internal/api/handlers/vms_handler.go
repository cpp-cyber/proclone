package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ADMIN: GetVMsHandler handles GET requests for retrieving all VMs on Proxmox
func (ph *ProxmoxHandler) GetVMsHandler(c *gin.Context) {
	vms, err := ph.service.GetVMs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve VMs", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"vms": vms})
}

// ADMIN: StartVMHandler handles POST requests for starting a VM on Proxmox
func (ph *ProxmoxHandler) StartVMHandler(c *gin.Context) {
	var req VMActionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ph.service.StartVM(req.Node, req.VMID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM started"})
}

// ADMIN: ShutdownVMHandler handles POST requests for shutting down a VM on Proxmox
func (ph *ProxmoxHandler) ShutdownVMHandler(c *gin.Context) {
	var req VMActionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ph.service.ShutdownVM(req.Node, req.VMID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to shutdown VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM shutdown"})
}

// ADMIN: RebootVMHandler handles POST requests for rebooting a VM on Proxmox
func (ph *ProxmoxHandler) RebootVMHandler(c *gin.Context) {
	var req VMActionRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := ph.service.RebootVM(req.Node, req.VMID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reboot VM", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM rebooted"})
}

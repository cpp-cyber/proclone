package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-contrib/sessions"
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

// ADMIN: GetUsersHandler handles GET requests for retrieving all users from Proxmox
func (ph *ProxmoxHandler) GetUsersHandler(c *gin.Context) {
	users, err := ph.service.GetUsers()
	if err != nil {
		log.Printf("Error retrieving users: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve users", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count": len(users),
		"users": users,
	})
}

// ADMIN: GetGroupsHandler handles GET requests for retrieving all groups from Proxmox
func (ph *ProxmoxHandler) GetGroupsHandler(c *gin.Context) {
	groups, err := ph.service.GetGroups()
	if err != nil {
		log.Printf("Error retrieving groups: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve groups", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":  len(groups),
		"groups": groups,
	})
}

// ADMIN: SetUserGroupsHandler handles POST requests for setting user groups in Proxmox
func (ph *ProxmoxHandler) SetUserGroupsHandler(c *gin.Context) {
	var req SetUserGroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.SetUserGroups(req.Username, req.Groups); err != nil {
		log.Printf("Error setting groups for user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set user groups", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "User groups updated"})
}

// ADMIN: CreateGroupsHandler handles POST requests for creating groups in Proxmox
func (ph *ProxmoxHandler) CreateGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []string
	var created []string

	for _, groupName := range req.Groups {
		if err := ph.service.CreateGroup(groupName, ""); err != nil {
			log.Printf("Error creating group %s: %v", groupName, err)
			errors = append(errors, fmt.Sprintf("Failed to create group %s: %v", groupName, err))
		} else {
			created = append(created, groupName)
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusPartialContent, gin.H{
			"status":  "Partial success",
			"created": created,
			"errors":  errors,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Groups created", "created": created})
}

// ADMIN: AddUsersHandler handles POST requests for adding users to a group in Proxmox
func (ph *ProxmoxHandler) AddUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.AddUsersToGroup(req.Group, req.Usernames); err != nil {
		log.Printf("Error adding users to group %s: %v", req.Group, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add users to group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Users added to group"})
}

// ADMIN: RemoveUsersHandler handles POST requests for removing users from a group in Proxmox
func (ph *ProxmoxHandler) RemoveUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.RemoveUsersFromGroup(req.Group, req.Usernames); err != nil {
		log.Printf("Error removing users from group %s: %v", req.Group, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove users from group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Users removed from group"})
}

// ADMIN: DeleteGroupsHandler handles POST requests for deleting groups in Proxmox
func (ph *ProxmoxHandler) DeleteGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []string
	var deleted []string

	for _, groupName := range req.Groups {
		if err := ph.service.DeleteGroup(groupName); err != nil {
			log.Printf("Error deleting group %s: %v", groupName, err)
			errors = append(errors, fmt.Sprintf("Failed to delete group %s: %v", groupName, err))
		} else {
			deleted = append(deleted, groupName)
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusPartialContent, gin.H{
			"status":  "Partial success",
			"deleted": deleted,
			"errors":  errors,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Groups deleted", "deleted": deleted})
}

func (ph *ProxmoxHandler) EditGroupHandler(c *gin.Context) {
	var req EditGroupRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := ph.service.EditGroup(req.Group, req.Comment); err != nil {
		log.Printf("Error editing group %s: %v", req.Group, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to edit group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Group edited"})
}

func (ph *ProxmoxHandler) GetVMTemplatesHandler(c *gin.Context) {
	vmTemplates, err := ph.service.GetVMTemplates()
	if err != nil {
		log.Printf("Error getting VM templates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get VM templates", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VM templates retrieved", "vm_templates": vmTemplates})
}

func (ph *ProxmoxHandler) GetProxmoxTemplatePoolsHandler(c *gin.Context) {
	templatePools, err := ph.service.GetTemplatePools()
	if err != nil {
		log.Printf("Error getting template pools: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get template pools", "details": err.Error()})
		return
	}

	trimmedTemplatePools := make([]string, 0, len(templatePools))
	for _, pool := range templatePools {
		trimmedTemplatePools = append(trimmedTemplatePools, strings.Replace(pool, "kamino_template_", "", 1))
	}

	c.JSON(http.StatusOK, gin.H{"status": "Template pools retrieved", "template_pools": trimmedTemplatePools})
}

func (ph *ProxmoxHandler) GetUsedVNetsHandler(c *gin.Context) {
	vnets, err := ph.service.GetUsedVNets()
	if err != nil {
		log.Printf("Error getting VNets: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get VNets", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "VNets retrieved", "vnets": vnets})
}

func (ph *ProxmoxHandler) CreateTemplateHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("id").(string)

	var request CreateTemplateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	err := ph.service.CreateTemplatePool(username, request.Name, request.Router, request.VMs)
	if err != nil {
		log.Printf("Error creating template: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Template created"})
}

package handlers

import (
	"net/http"

	"github.com/cpp-cyber/proclone/internal/api/auth"
	"github.com/cpp-cyber/proclone/internal/cloning"
	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/cpp-cyber/proclone/internal/tools"
	"github.com/gin-gonic/gin"
)

// =================================================
// Handler Types
// =================================================

// AuthHandler handles HTTP authentication requests
type AuthHandler struct {
	authService    auth.Service
	ldapService    ldap.Service
	proxmoxService proxmox.Service
}

// CloningHandler holds the cloning service
type CloningHandler struct {
	Service  *cloning.CloningService
	dbClient *tools.DBClient
}

// DashboardHandler handles HTTP requests for dashboard operations
type DashboardHandler struct {
	authHandler    *AuthHandler
	proxmoxHandler *ProxmoxHandler
	cloningHandler *CloningHandler
}

// ProxmoxHandler handles HTTP requests for Proxmox operations
type ProxmoxHandler struct {
	service proxmox.Service
}

// =================================================
// API Request Types
// =================================================

type VMActionRequest struct {
	Node string `json:"node" binding:"required,min=1,max=100" validate:"alphanum"`
	VMID int    `json:"vmid" binding:"required,min=100,max=999999"`
}

type TemplateRequest struct {
	Template string `json:"template" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
}

type PublishTemplateRequest struct {
	Template cloning.KaminoTemplate `json:"template" binding:"required"`
}

type CloneRequest struct {
	Template string `json:"template" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
}

type GroupsRequest struct {
	Groups []string `json:"groups" binding:"required,min=1,dive,min=1,max=100" validate:"dive,alphanum,ascii"`
}

type AdminCloneRequest struct {
	Template  string   `json:"template" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
	Usernames []string `json:"usernames" binding:"omitempty,dive,min=1,max=100" validate:"dive,alphanum,ascii"`
	Groups    []string `json:"groups" binding:"omitempty,dive,min=1,max=100" validate:"dive,alphanum,ascii"`
}

type DeletePodRequest struct {
	Pod string `json:"pod" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
}

type AdminDeletePodRequest struct {
	Pods []string `json:"pods" binding:"required,min=1,dive,min=1,max=100" validate:"dive,alphanum,ascii"`
}

type UsernamePasswordRequest struct {
	Username string `json:"username" binding:"required,min=3,max=20" validate:"alphanum,ascii"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

type AdminCreateUserRequest struct {
	Users []UsernamePasswordRequest `json:"users" binding:"required,min=1,max=100,dive"`
}

type UsersRequest struct {
	Usernames []string `json:"usernames" binding:"required,min=1,dive,min=1,max=50" validate:"dive,alphanum,ascii"`
}

type ModifyGroupMembersRequest struct {
	Group     string   `json:"group" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
	Usernames []string `json:"usernames" binding:"required,min=1,dive,min=1,max=50" validate:"dive,alphanum,ascii"`
}

type SetUserGroupsRequest struct {
	Username string   `json:"username" binding:"required,min=3,max=20" validate:"alphanum,ascii"`
	Groups   []string `json:"groups" binding:"required,min=1,dive,min=1,max=100" validate:"dive,alphanum,ascii"`
}

type RenameGroupRequest struct {
	OldName string `json:"old_name" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
	NewName string `json:"new_name" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
}

type DashboardStats struct {
	UserCount              int `json:"users"`
	GroupCount             int `json:"groups"`
	PublishedTemplateCount int `json:"published_templates"`
	DeployedPodCount       int `json:"deployed_pods"`
	VirtualMachineCount    int `json:"vms"`
	ClusterResourceUsage   any `json:"cluster"`
}

// =================================================
// Private Functions
// =================================================

func validateAndBind(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Validation failed",
			"details": "Invalid request format or missing required fields",
		})
		return false
	}
	return true
}

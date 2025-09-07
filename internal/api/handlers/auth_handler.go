package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/api/auth"
	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// =================================================
// Login / Logout / Session Handlers
// =================================================

// NewAuthHandler creates a new authentication handler
func NewAuthHandler() (*AuthHandler, error) {
	authService, err := auth.NewAuthService()
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	ldapService, err := ldap.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	proxmoxService, err := proxmox.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create proxmox service: %w", err)
	}

	log.Println("Auth handler initialized")

	return &AuthHandler{
		authService:    authService,
		ldapService:    ldapService,
		proxmoxService: proxmoxService,
	}, nil
}

// LoginHandler handles the login POST request
func (h *AuthHandler) LoginHandler(c *gin.Context) {
	var req UsernamePasswordRequest
	if !validateAndBind(c, &req) {
		return
	}

	// Authenticate user
	valid, err := h.authService.Authenticate(req.Username, req.Password)
	if err != nil {
		log.Printf("Authentication failed for user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Create session
	session := sessions.Default(c)
	session.Set("id", req.Username)

	// Check if user is admin
	isAdmin, err := h.authService.IsAdmin(req.Username)
	if err != nil {
		log.Printf("Error checking admin status for user %s: %v", req.Username, err)
		isAdmin = false
	}
	session.Set("isAdmin", isAdmin)

	if err := session.Save(); err != nil {
		log.Printf("Failed to save session for user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"isAdmin": isAdmin,
	})
}

// LogoutHandler handles user logout
func (h *AuthHandler) LogoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()

	if err := session.Save(); err != nil {
		log.Printf("Failed to clear session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Successfully logged out"})
}

// SessionHandler returns current session information for authenticated users
func (h *AuthHandler) SessionHandler(c *gin.Context) {
	session := sessions.Default(c)

	// Since this is under private routes, AuthRequired middleware ensures session exists
	id := session.Get("id")
	isAdmin := session.Get("isAdmin")

	// Convert isAdmin to bool, defaulting to false if not set
	adminStatus := false
	if isAdmin != nil {
		adminStatus = isAdmin.(bool)
	}

	c.JSON(http.StatusOK, gin.H{
		"authenticated": true,
		"username":      id.(string),
		"isAdmin":       adminStatus,
	})
}

func (h *AuthHandler) RegisterHandler(c *gin.Context) {
	var req UsernamePasswordRequest
	if !validateAndBind(c, &req) {
		return
	}

	// Check if the username already exists
	var userDN = ""
	userDN, err := h.ldapService.GetUserDN(req.Username)
	if userDN != "" {
		log.Printf("Attempt to register existing username: %s", req.Username)
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
		return
	}
	if err != nil {
		// Ignore since this error is (most likely) stating that the user does not exist
	}

	// Create user
	if err := h.ldapService.CreateAndRegisterUser(ldap.UserRegistrationInfo(req)); err != nil {
		log.Printf("Failed to create user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User registered successfully"})
}

// =================================================
// User Handlers
// =================================================

// ADMIN: GetUsersHandler returns a list of all users
func (h *AuthHandler) GetUsersHandler(c *gin.Context) {
	users, err := h.ldapService.GetUsers()
	if err != nil {
		log.Printf("Failed to retrieve users: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve users"})
		return
	}

	var adminCount = 0
	var disabledCount = 0
	for _, user := range users {
		if user.IsAdmin {
			adminCount++
		}
		if !user.Enabled {
			disabledCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"users":          users,
		"count":          len(users),
		"disabled_count": disabledCount,
		"admin_count":    adminCount,
	})
}

// ADMIN: CreateUsersHandler creates new user(s)
func (h *AuthHandler) CreateUsersHandler(c *gin.Context) {
	var req AdminCreateUserRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	// Create users in AD
	for _, user := range req.Users {
		if err := h.ldapService.CreateAndRegisterUser(ldap.UserRegistrationInfo(user)); err != nil {
			errors = append(errors, fmt.Errorf("failed to create user %s: %v", user.Username, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to create users: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create users", "details": errors})
		return
	}

	// Sync users to Proxmox
	if err := h.proxmoxService.SyncUsers(); err != nil {
		log.Printf("Failed to sync users with Proxmox: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync users with Proxmox", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Users created successfully"})
}

// ADMIN: DeleteUsersHandler deletes existing user(s)
func (h *AuthHandler) DeleteUsersHandler(c *gin.Context) {
	var req UsersRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	// Delete users in AD
	for _, username := range req.Usernames {
		if err := h.ldapService.DeleteUser(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to delete users: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete users", "details": errors})
		return
	}

	// Sync users to Proxmox
	if err := h.proxmoxService.SyncUsers(); err != nil {
		log.Printf("Failed to sync users with Proxmox: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync users with Proxmox", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users deleted successfully"})
}

// ADMIN: EnableUsersHandler enables existing user(s)
func (h *AuthHandler) EnableUsersHandler(c *gin.Context) {
	var req UsersRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	for _, username := range req.Usernames {
		if err := h.ldapService.EnableUserAccount(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to enable user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to enable users: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable users", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users enabled successfully"})
}

// ADMIN: DisableUsersHandler disables existing user(s)
func (h *AuthHandler) DisableUsersHandler(c *gin.Context) {
	var req UsersRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	for _, username := range req.Usernames {
		if err := h.ldapService.DisableUserAccount(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to disable user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to disable users: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable users", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users disabled successfully"})
}

// =================================================
// Group Handlers
// =================================================

// ADMIN: SetUserGroupsHandler sets the groups for an existing user
func (h *AuthHandler) SetUserGroupsHandler(c *gin.Context) {
	var req SetUserGroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := h.ldapService.SetUserGroups(req.Username, req.Groups); err != nil {
		log.Printf("Failed to set groups for user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set user groups", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User groups updated successfully"})
}

func (h *AuthHandler) GetGroupsHandler(c *gin.Context) {
	groups, err := h.ldapService.GetGroups()
	if err != nil {
		log.Printf("Failed to retrieve groups: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve groups"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
		"count":  len(groups),
	})
}

// ADMIN: CreateGroupsHandler creates new group(s)
func (h *AuthHandler) CreateGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	// Create groups in AD
	for _, group := range req.Groups {
		if err := h.ldapService.CreateGroup(group); err != nil {
			errors = append(errors, fmt.Errorf("failed to create group %s: %v", group, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to create groups: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create groups", "details": errors})
		return
	}

	// Sync groups to Proxmox
	if err := h.proxmoxService.SyncGroups(); err != nil {
		log.Printf("Failed to sync groups with Proxmox: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync groups with Proxmox", "details": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Groups created successfully"})
}

func (h *AuthHandler) RenameGroupHandler(c *gin.Context) {
	var req RenameGroupRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := h.ldapService.RenameGroup(req.OldName, req.NewName); err != nil {
		log.Printf("Failed to rename group %s to %s: %v", req.OldName, req.NewName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Group renamed successfully"})
}

func (h *AuthHandler) DeleteGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if !validateAndBind(c, &req) {
		return
	}

	var errors []error

	// Delete groups in AD
	for _, group := range req.Groups {
		if err := h.ldapService.DeleteGroup(group); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete group %s: %v", group, err))
		}
	}

	if len(errors) > 0 {
		log.Printf("Failed to delete groups: %v", errors)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete groups", "details": errors})
		return
	}

	// Sync groups to Proxmox
	if err := h.proxmoxService.SyncGroups(); err != nil {
		log.Printf("Failed to sync groups with Proxmox: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to sync groups with Proxmox", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Groups deleted successfully"})
}

func (h *AuthHandler) AddUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := h.ldapService.AddUsersToGroup(req.Group, req.Usernames); err != nil {
		log.Printf("Failed to add users to group %s: %v", req.Group, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add users to group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users added to group successfully"})
}

func (h *AuthHandler) RemoveUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if !validateAndBind(c, &req) {
		return
	}

	if err := h.ldapService.RemoveUsersFromGroup(req.Group, req.Usernames); err != nil {
		log.Printf("Failed to remove users from group %s: %v", req.Group, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove users from group", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users removed from group successfully"})
}

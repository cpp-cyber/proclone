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
	proxmoxServiceInterface, err := proxmox.NewService()
	if err != nil {
		return nil, fmt.Errorf("failed to create proxmox service: %w", err)
	}

	// Type assert to get concrete type for auth service
	proxmoxService, ok := proxmoxServiceInterface.(*proxmox.ProxmoxService)
	if !ok {
		return nil, fmt.Errorf("failed to convert proxmox service to concrete type")
	}

	authService, err := auth.NewAuthService(proxmoxService)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	ldapService, err := ldap.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	log.Println("Auth handler initialized")

	return &AuthHandler{
		authService:    authService,
		ldapService:    ldapService,
		proxmoxService: proxmoxServiceInterface,
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

package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/auth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// AuthHandler handles HTTP authentication requests
type AuthHandler struct {
	authService auth.Service
}

// NewAuthHandler creates a new authentication handler
func NewAuthHandler() (*AuthHandler, error) {
	authService, err := auth.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	log.Println("Auth handler initialized")

	return &AuthHandler{
		authService: authService,
	}, nil
}

// LoginHandler handles the login POST request
func (h *AuthHandler) LoginHandler(c *gin.Context) {
	var loginReq struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&loginReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Authenticate user
	valid, err := h.authService.Authenticate(loginReq.Username, loginReq.Password)
	if err != nil {
		log.Printf("Authentication failed for user %s: %v", loginReq.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Authentication failed"})
		return
	}

	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Create session
	session := sessions.Default(c)
	session.Set("id", loginReq.Username)

	// Check if user is admin
	isAdmin, err := h.authService.IsAdmin(loginReq.Username)
	if err != nil {
		log.Printf("Error checking admin status for user %s: %v", loginReq.Username, err)
		isAdmin = false
	}
	session.Set("isAdmin", isAdmin)

	if err := session.Save(); err != nil {
		log.Printf("Failed to save session for user %s: %v", loginReq.Username, err)
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
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Check if the username already exists
	var userDN = ""
	userDN, err := h.authService.GetUserDN(req.Username)
	if userDN != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "Username already exists"})
		return
	}
	if err != nil {
		// Ignore since this error is (most likely) stating that the user does not exist
	}

	// Create user
	if err := h.authService.CreateAndRegisterUser(auth.UserRegistrationInfo(req)); err != nil {
		log.Printf("Failed to create user %s: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User registered successfully"})
}

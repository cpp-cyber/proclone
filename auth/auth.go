package auth

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// struct to hold username and password received from post request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// called by /api/login post request
func LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	username := strings.TrimSpace(req.Username)
	password := req.Password

	// return error if either username or password are empty
	if username == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password are required"})
		return
	}

	// Connect to LDAP
	ldapConn, err := ConnectToLDAP()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("LDAP connection failed: %v", err)})
		return
	}
	defer ldapConn.Close()

	// Authenticate user
	_, groups, err := ldapConn.AuthenticateUser(username, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// Check if user is admin
	isAdmin := CheckIfAdmin(groups)

	log.Println("logging user membership: ", groups)
	for _, group := range groups {
		log.Println("User is a member of: ", group)
	}

	// create session
	session := sessions.Default(c)
	session.Set("authenticated", true)
	session.Set("username", username)
	session.Set("is_admin", isAdmin)
	session.Save()

	c.JSON(http.StatusOK, gin.H{"message": "Login successful"})
}

// handle clearing session cookies
func LogoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// check logged in profile
func ProfileHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"username": username,
		"isAdmin":  isAdmin,
	})
}

// check if user is authenticated
func IsAuthenticated(c *gin.Context) (bool, string) {
	session := sessions.Default(c)
	auth, ok := session.Get("authenticated").(bool)
	if !ok || !auth {
		return false, ""
	}
	username, _ := session.Get("username").(string)
	return true, username
}

// check if user is in "Domain Admins" group
func isAdmin(c *gin.Context) bool {
	session := sessions.Default(c)
	isAdmin, _ := session.Get("is_admin").(bool)
	return isAdmin
}

// api endpoint that returns true if user is already authenticated
func SessionHandler(c *gin.Context) {
	if ok, username := IsAuthenticated(c); ok {
		is_admin := isAdmin(c)
		c.JSON(http.StatusOK, gin.H{
			"authenticated": true,
			"username":      username,
			"isAdmin":       is_admin,
		})
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"authenticated": false})
	}
}

// auth protected routes helper function
func AuthRequired(c *gin.Context) {
	if ok, _ := IsAuthenticated(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return
	}
	c.Next()
}

// admin protected routes helper function
func AdminRequired(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
		c.Abort()
		return
	}
	c.Next()
}

package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-ldap/ldap/v3"
)

// struct to hold username and password received from post request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// called by /api/login post request
func loginHandler(c *gin.Context) {
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

	// LDAP stuff
	ldapServer := os.Getenv("LDAP_SERVER")
	baseDN := os.Getenv("LDAP_BASE_DN")
	bindDN := os.Getenv("LDAP_BIND_DN")
	bindPassword := os.Getenv("LDAP_BIND_PASSWORD")

	// for deployment debugging purposes
	if ldapServer == "" || baseDN == "" || bindDN == "" || bindPassword == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "LDAP configuration is missing"})
		return
	}

	// connect to LDAP server
	l, err := ldap.DialURL("ldap://" + ldapServer + ":389")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("LDAP bind to %s failed", ldapServer)})
		return
	}

	// make sure connection closes at function return even if error occurs
	defer l.Close()

	// First bind as service account
	err = l.Bind(bindDN, bindPassword)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid ldap service account"})
		return
	}

	// Define search request
	searchRequest := ldap.NewSearchRequest(
		baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(sAMAccountName=%s)", username),
		[]string{"dn"},
		nil,
	)

	// search for user
	sr, err := l.Search(searchRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found in LDAP"})
		return
	}

	// handle user not found
	if len(sr.Entries) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found or multiple users found"})
		return
	}

	userDN := sr.Entries[0].DN

	// bind as user to verify password
	err = l.Bind(userDN, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	isAdmin := false

	// check if user is in "Domain Admins"
	groups := sr.Entries[0].GetAttributeValues("memberOf")
	for _, group := range groups {
		if strings.Contains(strings.ToLower(group), "cn=domain admins") {
			isAdmin = true
			break
		}
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
func logoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.JSON(http.StatusOK, gin.H{"message": "Logged out"})
}

// check logged in profile
func profileHandler(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	if username == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": username,
		"isAdmin": isAdmin,
	})
}

// check if user is authenticated
func isAuthenticated(c *gin.Context) (bool, string) {
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
func sessionHandler(c *gin.Context) {
	if ok, username := isAuthenticated(c); ok {
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
func authRequired(c *gin.Context) {
	if ok, _ := isAuthenticated(c); !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return
	}
	c.Next()
}

// admin protected routes helper function
func adminRequired(c *gin.Context) {
	if !isAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden"})
		c.Abort()
		return
	}
	c.Next()
}

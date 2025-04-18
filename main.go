package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-ldap/ldap/v3"
)

// struct to hold username and password received from post request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// init the environment
func init() {
	_ = godotenv.Load()
}


func main() {
	r := gin.Default()

	// store session cookie
	// **IN PROD USE REAL SECURE KEY**
	store := cookie.NewStore([]byte(os.Getenv("SECRET_KEY")))
	r.Use(sessions.Sessions("mysession", store))

	// export public route
	r.POST("/api/login", loginHandler)

	// authenticated routes
	auth := r.Group("/")
	auth.Use(authRequired)
	auth.GET("/api/profile", profileHandler)
	auth.POST("/api/logout", logoutHandler)

	// get port to run server on via. PC_PORT env variable
	port := os.Getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
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
		fmt.Sprintf("(&(objectClass=person)(uid=%s))", username),
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

	userDN := sr.Entries[0].dn

	// bind as user to verify password
	err = l.Bind(userDN, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	// create session
	session := sessions.Default(c)
	session.Set("authenticated", true)
	session.Set("username", username)
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
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Hello, %s!", username)})
}

// protect routes helper function
func authRequired(c *gin.Context) {
	session := sessions.Default(c)
	if auth, ok := session.Get("authenticated").(bool); !ok || !auth {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return
	}
	c.Next()
}

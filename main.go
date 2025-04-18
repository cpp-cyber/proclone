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

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

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
	auth.GET("/profile", profileHandler)
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
	domain := os.Getenv("NETBIOS_NAME")
	ldapServer := os.Getenv("LDAP_SERVER")

	l, err := ldap.DialURL("ldap://" + ldapServer + ":389")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "LDAP connection failed" + domain + " " + ldapServer})
		return
	}
	defer l.Close()

	userPrincipal := fmt.Sprintf("%s@%s", username, domain)
	err = l.Bind(userPrincipal, password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

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

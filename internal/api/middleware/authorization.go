package middleware

import (
	"log"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/api/auth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// authRequired provides authentication middleware for ensuring that a user is logged in.
func AuthRequired(c *gin.Context) {
	session := sessions.Default(c)
	id := session.Get("id")
	if id == nil {
		c.String(http.StatusUnauthorized, "Unauthorized")
		c.Abort()
		return
	}
	c.Next()
}

func AdminRequired(authService auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := session.Get("id")
		if id == nil {
			c.String(http.StatusUnauthorized, "Unauthorized")
			c.Abort()
			return
		}

		username := id.(string)

		// Verify with Proxmox
		isAdmin, err := authService.IsAdmin(username)
		if err != nil {
			log.Printf("Error verifying admin status for user %s: %v", username, err)
			c.String(http.StatusInternalServerError, "Failed to verify permissions")
			c.Abort()
			return
		}

		if !isAdmin {
			c.String(http.StatusForbidden, "Admin access required")
			c.Abort()
			return
		}

		c.Next()
	}
}

// CreatorOrAdminRequired provides authorization middleware for template operations
func CreatorOrAdminRequired(authService auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := session.Get("id")
		if id == nil {
			c.String(http.StatusUnauthorized, "Unauthorized")
			c.Abort()
			return
		}

		username := id.(string)

		// Verify with Proxmox for template operations
		isAdmin, err := authService.IsAdmin(username)
		if err != nil {
			log.Printf("Error verifying admin status for user %s: %v", username, err)
			c.String(http.StatusInternalServerError, "Failed to verify permissions")
			c.Abort()
			return
		}

		isCreator, err := authService.IsCreator(username)
		if err != nil {
			log.Printf("Error verifying creator status for user %s: %v", username, err)
			c.String(http.StatusInternalServerError, "Failed to verify permissions")
			c.Abort()
			return
		}

		if !isAdmin && !isCreator {
			c.String(http.StatusForbidden, "Creator or Admin access required")
			c.Abort()
			return
		}

		c.Next()
	}
}

func GetUser(c *gin.Context) string {
	userID := sessions.Default(c).Get("id")
	if userID != nil {
		return userID.(string)
	}
	return ""
}

func Logout(c *gin.Context) {
	session := sessions.Default(c)
	id := session.Get("id")
	if id == nil {
		c.JSON(http.StatusOK, gin.H{"message": "No session."})
		return
	}
	session.Delete("id")
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Successfully logged out!"})
}

func CORSMiddleware(fqdn string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Content-Type", "application/json; text/event-stream")
		c.Writer.Header().Set("Access-Control-Allow-Origin", fqdn)
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, Origin")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(200)
		}

		c.Next()
	}
}

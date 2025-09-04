package middleware

import (
	"net/http"

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

func AdminRequired(c *gin.Context) {
	session := sessions.Default(c)
	id := session.Get("id")
	if id == nil {
		c.String(http.StatusUnauthorized, "Unauthorized")
		c.Abort()
		return
	}

	isAdmin := session.Get("isAdmin")
	if isAdmin == nil || !isAdmin.(bool) {
		c.String(http.StatusForbidden, "Admin access required")
		c.Abort()
		return
	}

	c.Next()
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

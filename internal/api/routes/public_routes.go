package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerPublicRoutes defines all routes accessible without authentication
func registerPublicRoutes(g *gin.RouterGroup, authHandler *handlers.AuthHandler, cloningHandler *handlers.CloningHandler) {
	// GET Requests
	g.GET("/health", handlers.HealthCheckHandler(authHandler, cloningHandler))
	g.POST("/login", authHandler.LoginHandler)
	// g.POST("/register", authHandler.RegisterHandler)
}

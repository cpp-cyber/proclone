package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/cpp-cyber/proclone/internal/api/middleware"
	"github.com/gin-gonic/gin"
)

// RegisterRoutes sets up all API routes with their respective middleware and handlers
func RegisterRoutes(r *gin.Engine, authHandler *handlers.AuthHandler, proxmoxHandler *handlers.ProxmoxHandler, cloningHandler *handlers.CloningHandler) {
	// Create centralized dashboard handler
	dashboardHandler := handlers.NewDashboardHandler(authHandler, proxmoxHandler, cloningHandler)

	// Public routes (no authentication required)
	public := r.Group("/api/v1")
	registerPublicRoutes(public, authHandler, cloningHandler)

	// Private routes (authentication required)
	private := r.Group("/api/v1")
	private.Use(middleware.AuthRequired)
	registerPrivateRoutes(private, authHandler, cloningHandler, dashboardHandler)

	// Admin routes (authentication + admin privileges required)
	admin := r.Group("/api/v1/admin")
	admin.Use(middleware.AdminRequired)
	registerAdminRoutes(admin, authHandler, proxmoxHandler, cloningHandler, dashboardHandler)
}

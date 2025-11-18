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

	// Get auth service from handler for middleware
	authService := authHandler.GetAuthService()

	// Public routes (no authentication required)
	public := r.Group("/api/v1")
	registerPublicRoutes(public, authHandler, cloningHandler)

	// Private routes (authentication required)
	private := r.Group("/api/v1")
	private.Use(middleware.AuthRequired)
	registerPrivateRoutes(private, authHandler, cloningHandler, dashboardHandler)

	// Creator routes (authentication + creator OR admin privileges required)
	// Template management operations accessible to both creators and admins
	creator := r.Group("/api/v1/creator")
	creator.Use(middleware.CreatorOrAdminRequired(authService))
	registerCreatorRoutes(creator, proxmoxHandler, cloningHandler)

	// Admin routes (authentication + admin privileges required)
	// User/group management and system operations
	admin := r.Group("/api/v1/admin")
	admin.Use(middleware.AdminRequired(authService))
	registerAdminRoutes(admin, authHandler, proxmoxHandler, cloningHandler, dashboardHandler)
}

package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerPrivateRoutes defines all routes accessible to authenticated users
func registerPrivateRoutes(g *gin.RouterGroup, authHandler *handlers.AuthHandler, cloningHandler *handlers.CloningHandler, dashboardHandler *handlers.DashboardHandler) {
	// GET Requests
	g.GET("/dashboard", dashboardHandler.GetUserDashboardStatsHandler)
	g.GET("/session", authHandler.SessionHandler)
	g.GET("/pods", cloningHandler.GetPodsHandler)
	g.GET("/templates", cloningHandler.GetTemplatesHandler)
	g.GET("/template/image/:filename", cloningHandler.GetTemplateImageHandler)

	// POST Requests
	g.POST("/logout", authHandler.LogoutHandler)
	g.POST("/pod/delete", cloningHandler.DeletePodHandler)
	g.POST("/template/clone", cloningHandler.CloneTemplateHandler)
}

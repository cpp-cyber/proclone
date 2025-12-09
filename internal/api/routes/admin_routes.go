package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerAdminRoutes defines all routes accessible ONLY to admin users
// Template operations have been moved to creator routes (accessible by both admins and creators)
func registerAdminRoutes(g *gin.RouterGroup, authHandler *handlers.AuthHandler, proxmoxHandler *handlers.ProxmoxHandler, cloningHandler *handlers.CloningHandler, dashboardHandler *handlers.DashboardHandler) {
	// Admin dashboard and cluster management
	g.GET("/dashboard", dashboardHandler.GetAdminDashboardStatsHandler)
	g.GET("/cluster", proxmoxHandler.GetClusterResourceUsageHandler)
	g.GET("/vnets", proxmoxHandler.GetUsedVNetsHandler)
	g.GET("/vms", proxmoxHandler.GetVMsHandler)
	g.GET("/pods", cloningHandler.AdminGetPodsHandler)

	// User management (admin only)
	g.GET("/users", authHandler.GetUsersHandler)
	g.POST("/users/create", authHandler.CreateUsersHandler)
	g.POST("/users/delete", authHandler.DeleteUsersHandler)
	g.POST("/users/enable", authHandler.EnableUsersHandler)
	g.POST("/users/disable", authHandler.DisableUsersHandler)
	g.POST("/user/groups", authHandler.SetUserGroupsHandler)

	// Group management (admin only)
	g.GET("/groups", authHandler.GetGroupsHandler)
	g.POST("/groups/create", authHandler.CreateGroupsHandler)
	g.POST("/group/members/add", authHandler.AddUsersHandler)
	g.POST("/group/members/remove", authHandler.RemoveUsersHandler)
	g.POST("/group/rename", authHandler.RenameGroupHandler)
	g.POST("/groups/delete", authHandler.DeleteGroupsHandler)

	// VM management (admin only)
	g.POST("/vm/start", proxmoxHandler.StartVMHandler)
	g.POST("/vm/shutdown", proxmoxHandler.ShutdownVMHandler)
	g.POST("/vm/reboot", proxmoxHandler.RebootVMHandler)

	// Pod management (admin only)
	g.POST("/pods/delete", cloningHandler.AdminDeletePodHandler)

	// Bulk template deployment (admin only)
	g.POST("/templates/clone", cloningHandler.AdminCloneTemplateHandler)
}

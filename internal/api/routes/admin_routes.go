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
	g.GET("/users", proxmoxHandler.GetUsersHandler)
	g.POST("/users/create", authHandler.CreateUsersHandler)
	g.POST("/users/delete", authHandler.DeleteUsersHandler)
	g.POST("/user/groups", proxmoxHandler.SetUserGroupsHandler)

	// Group management (admin only)
	g.GET("/groups", proxmoxHandler.GetGroupsHandler)
	g.POST("/groups/create", proxmoxHandler.CreateGroupsHandler)
	g.POST("/group/members/add", proxmoxHandler.AddUsersHandler)
	g.POST("/group/members/remove", proxmoxHandler.RemoveUsersHandler)
	g.POST("/group/edit", proxmoxHandler.EditGroupHandler)
	g.POST("/groups/delete", proxmoxHandler.DeleteGroupsHandler)

	// VM management (admin only)
	g.POST("/vm/start", proxmoxHandler.StartVMHandler)
	g.POST("/vm/shutdown", proxmoxHandler.ShutdownVMHandler)
	g.POST("/vm/reboot", proxmoxHandler.RebootVMHandler)

	// Pod management (admin only)
	g.POST("/pods/delete", cloningHandler.AdminDeletePodHandler)

	// Bulk template deployment (admin only)
	g.POST("/templates/clone", cloningHandler.AdminCloneTemplateHandler)
}

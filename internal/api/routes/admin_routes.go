package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerAdminRoutes defines all routes accessible to admin users
func registerAdminRoutes(g *gin.RouterGroup, authHandler *handlers.AuthHandler, proxmoxHandler *handlers.ProxmoxHandler, cloningHandler *handlers.CloningHandler) {
	// Create dashboard handler
	dashboardHandler := handlers.NewDashboardHandler(authHandler, proxmoxHandler, cloningHandler)

	// GET Requests
	g.GET("/dashboard", dashboardHandler.GetDashboardStatsHandler)
	g.GET("/cluster", proxmoxHandler.GetClusterResourceUsageHandler)
	g.GET("/users", authHandler.GetUsersHandler)
	g.GET("/groups", authHandler.GetGroupsHandler)
	g.GET("/vms", proxmoxHandler.GetVMsHandler)
	g.GET("/pods", cloningHandler.AdminGetPodsHandler)
	g.GET("/templates", cloningHandler.AdminGetTemplatesHandler)
	g.GET("/templates/unpublished", cloningHandler.GetUnpublishedTemplatesHandler)

	// POST Requests
	g.POST("/users/create", authHandler.CreateUsersHandler)
	g.POST("/users/delete", authHandler.DeleteUsersHandler)
	g.POST("/users/enable", authHandler.EnableUsersHandler)
	g.POST("/users/disable", authHandler.DisableUsersHandler)
	g.POST("/user/groups", authHandler.SetUserGroupsHandler)
	g.POST("/groups/create", authHandler.CreateGroupsHandler)
	g.POST("/group/members/add", authHandler.AddUsersHandler)
	g.POST("/group/members/remove", authHandler.RemoveUsersHandler)
	g.POST("/group/rename", authHandler.RenameGroupHandler)
	g.POST("/groups/delete", authHandler.DeleteGroupsHandler)
	g.POST("/vm/start", proxmoxHandler.StartVMHandler)
	g.POST("/vm/shutdown", proxmoxHandler.ShutdownVMHandler)
	g.POST("/vm/reboot", proxmoxHandler.RebootVMHandler)
	g.POST("/pods/delete", cloningHandler.AdminDeletePodHandler)
	g.POST("/template/publish", cloningHandler.PublishTemplateHandler)
	g.POST("/template/delete", cloningHandler.DeleteTemplateHandler)
	g.POST("/template/visibility", cloningHandler.ToggleTemplateVisibilityHandler)
	g.POST("/template/image/upload", cloningHandler.UploadTemplateImageHandler)
	g.POST("/templates/clone", cloningHandler.AdminCloneTemplateHandler)
}

package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerAdminRoutes defines all routes accessible to admin users
func registerAdminRoutes(g *gin.RouterGroup, authHandler *handlers.AuthHandler, proxmoxHandler *handlers.ProxmoxHandler, cloningHandler *handlers.CloningHandler, dashboardHandler *handlers.DashboardHandler) {
	// GET Requests
	g.GET("/dashboard", dashboardHandler.GetAdminDashboardStatsHandler)
	g.GET("/cluster", proxmoxHandler.GetClusterResourceUsageHandler)
	g.GET("/users", proxmoxHandler.GetUsersHandler)
	g.GET("/groups", proxmoxHandler.GetGroupsHandler)
	g.GET("/vms", proxmoxHandler.GetVMsHandler)
	g.GET("/vnets", proxmoxHandler.GetUsedVNetsHandler)
	g.GET("/pods", cloningHandler.AdminGetPodsHandler)
	g.GET("/templates", cloningHandler.AdminGetTemplatesHandler)
	g.GET("/templates/unpublished", cloningHandler.GetUnpublishedTemplatesHandler)
	g.GET("/templates/vms", proxmoxHandler.GetVMTemplatesHandler)
	g.GET("/templates/proxmox", proxmoxHandler.GetProxmoxTemplatePoolsHandler)

	// POST Requests
	g.POST("/users/create", authHandler.CreateUsersHandler)
	g.POST("/users/delete", authHandler.DeleteUsersHandler)
	g.POST("/user/groups", proxmoxHandler.SetUserGroupsHandler)
	g.POST("/groups/create", proxmoxHandler.CreateGroupsHandler)
	g.POST("/group/members/add", proxmoxHandler.AddUsersHandler)
	g.POST("/group/members/remove", proxmoxHandler.RemoveUsersHandler)
	g.POST("/group/edit", proxmoxHandler.EditGroupHandler)
	g.POST("/groups/delete", proxmoxHandler.DeleteGroupsHandler)
	g.POST("/vm/start", proxmoxHandler.StartVMHandler)
	g.POST("/vm/shutdown", proxmoxHandler.ShutdownVMHandler)
	g.POST("/vm/reboot", proxmoxHandler.RebootVMHandler)
	g.POST("/pods/delete", cloningHandler.AdminDeletePodHandler)
	g.POST("/template/publish", cloningHandler.PublishTemplateHandler)
	g.POST("/template/create", proxmoxHandler.CreateTemplateHandler)
	g.POST("/template/edit", cloningHandler.EditTemplateHandler)
	g.POST("/template/delete", cloningHandler.DeleteTemplateHandler)
	g.POST("/template/visibility", cloningHandler.ToggleTemplateVisibilityHandler)
	g.POST("/template/image/upload", cloningHandler.UploadTemplateImageHandler)
	g.POST("/templates/clone", cloningHandler.AdminCloneTemplateHandler)
}

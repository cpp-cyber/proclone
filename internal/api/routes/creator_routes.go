package routes

import (
	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/gin-gonic/gin"
)

// registerCreatorRoutes defines all routes accessible to both creators and admins
func registerCreatorRoutes(g *gin.RouterGroup, proxmoxHandler *handlers.ProxmoxHandler, cloningHandler *handlers.CloningHandler) {
	// Template management operations (create, publish, edit, delete)
	g.POST("/template/publish", cloningHandler.PublishTemplateHandler)
	g.POST("/template/create", proxmoxHandler.CreateTemplateHandler)
	g.POST("/template/edit", cloningHandler.EditTemplateHandler)
	g.POST("/template/delete", cloningHandler.DeleteTemplateHandler)
	g.POST("/template/visibility", cloningHandler.ToggleTemplateVisibilityHandler)
	g.POST("/template/image/upload", cloningHandler.UploadTemplateImageHandler)

	// Template viewing operations
	g.GET("/templates", cloningHandler.AdminGetTemplatesHandler)
	g.GET("/templates/unpublished", cloningHandler.GetUnpublishedTemplatesHandler)
	g.GET("/templates/vms", proxmoxHandler.GetVMTemplatesHandler)
	g.GET("/templates/proxmox", proxmoxHandler.GetProxmoxTemplatePoolsHandler)
}

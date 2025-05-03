package main

import (
	"log"
	"os"

	"github.com/P-E-D-L/proclone/auth"
	"github.com/P-E-D-L/proclone/proxmox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// init the environment
func init() {
	_ = godotenv.Load()
}

func main() {
	r := gin.Default()

	// store session cookie
	// **IN PROD USE REAL SECURE KEY**
	store := cookie.NewStore([]byte(os.Getenv("SECRET_KEY")))

	// further cookie security
	store.Options(sessions.Options{
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   true,
	})

	r.Use(sessions.Sessions("mysession", store))

	// export public route
	r.POST("/api/login", auth.LoginHandler)

	// authenticated routes
	user := r.Group("/api")
	user.Use(auth.AuthRequired)
	user.GET("/profile", auth.ProfileHandler)
	user.GET("/session", auth.SessionHandler)
	user.POST("/logout", auth.LogoutHandler)

	// Proxmox User Template endpoints
	user.GET("/proxmox/templates", proxmox.GetAvailableTemplates)

	// Proxmox Pod endpoints
	user.POST("/proxmox/pods/clone", proxmox.CloneTemplateToPod)

	// admin routes
	admin := user.Group("/admin")
	admin.Use(auth.AdminRequired)

	// Proxmox VM endpoints
	admin.GET("/proxmox/virtualmachines", proxmox.GetVirtualMachines)
	admin.POST("/proxmox/virtualmachines/shutdown", proxmox.PowerOffVirtualMachine)
	admin.POST("/proxmox/virtualmachines/start", proxmox.PowerOnVirtualMachine)

	// Proxmox resource monitoring endpoint
	admin.GET("/proxmox/resources", proxmox.GetProxmoxResources)

	// get port to run server on via. PC_PORT env variable
	port := os.Getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

package main

import (
	"log"
	"os"

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
	r.POST("/api/login", loginHandler)

	// authenticated routes
	auth := r.Group("/api")
	auth.Use(authRequired)
	auth.GET("/profile", profileHandler)
	auth.GET("/session", sessionHandler)
	auth.POST("/logout", logoutHandler)

	// admin routes
	admin := auth.Group("/admin")
	admin.Use(adminRequired)
	
	// Proxmox resource monitoring endpoint
	admin.GET("/proxmox/resources", getProxmoxResources)

	// get port to run server on via. PC_PORT env variable
	port := os.Getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

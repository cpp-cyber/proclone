package main

import (
	"log"

	"github.com/cpp-cyber/proclone/internal/api/handlers"
	"github.com/cpp-cyber/proclone/internal/api/routes"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Config holds all application configuration
type Config struct {
	Port          string `envconfig:"PORT" default:":8080"`
	SessionSecret string `envconfig:"SESSION_SECRET" default:"default-secret-key"`
}

// init the environment
func init() {
	_ = godotenv.Load()
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	// Load and parse configuration from environment variables
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		log.Fatalf("Failed to process environment configuration: %v", err)
	}

	r := gin.Default()

	// Setup session middleware
	store := cookie.NewStore([]byte(config.SessionSecret))
	r.Use(sessions.Sessions("session", store))

	// Initialize handlers
	authHandler, err := handlers.NewAuthHandler()
	if err != nil {
		log.Fatalf("Failed to initialize auth handler: %v", err)
	}

	proxmoxHandler, err := handlers.NewProxmoxHandler()
	if err != nil {
		log.Fatalf("Failed to initialize Proxmox handler: %v", err)
	}

	cloningHandler, err := handlers.NewCloningHandler()
	if err != nil {
		log.Fatalf("Failed to initialize cloning handler: %v", err)
	}

	routes.RegisterRoutes(r, authHandler, proxmoxHandler, cloningHandler)
	r.Run(config.Port)
}

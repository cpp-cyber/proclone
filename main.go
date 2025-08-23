package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/cpp-cyber/proclone/auth"
	"github.com/cpp-cyber/proclone/database"
	"github.com/cpp-cyber/proclone/proxmox"
	"github.com/cpp-cyber/proclone/proxmox/cloning"
	"github.com/cpp-cyber/proclone/proxmox/images"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"google.golang.org/grpc/credentials"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	serviceName  = os.Getenv("SERVICE_NAME")
	collectorURL = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	insecure     = os.Getenv("INSECURE_MODE")
)

// init the environment
func init() {
	_ = godotenv.Load()
}

func initTracer() func(context.Context) error {

	var secureOption otlptracegrpc.Option

	if strings.ToLower(insecure) == "false" || insecure == "0" || strings.ToLower(insecure) == "f" {
		secureOption = otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, ""))
	} else {
		secureOption = otlptracegrpc.WithInsecure()
	}

	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			secureOption,
			otlptracegrpc.WithEndpoint(collectorURL),
		),
	)

	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		log.Fatalf("Could not set resources: %v", err)
	}

	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resources),
		),
	)
	return exporter.Shutdown
}

func main() {

	cleanup := initTracer()
	defer cleanup(context.Background())

	// Ensure upload directory exists
	if err := os.MkdirAll(images.UploadDir, os.ModePerm); err != nil {
		log.Fatalf("failed to create upload dir: %v", err)
	}

	// Initialize database connection
	if err := database.InitializeDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.CloseDB()

	r := gin.Default()
	r.Use(otelgin.Middleware(serviceName))

	r.MaxMultipartMemory = 10 << 20 // 10 MiB

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
	user.GET("/proxmox/templates", cloning.GetAvailableTemplates)
	user.GET("/proxmox/templates/images/:filename", images.HandleGetFile)
	user.POST("/proxmox/templates/clone", cloning.CloneTemplateToPod)
	user.POST("/proxmox/pods/delete", cloning.DeletePod)

	// Proxmox Pod endpoints
	user.GET("/proxmox/pods", cloning.GetUserPods)

	// admin routes
	admin := user.Group("/admin")
	admin.Use(auth.AdminRequired)

	// Proxmox VM endpoints
	admin.GET("/proxmox/virtualmachines", proxmox.GetVirtualMachines)
	admin.POST("/proxmox/virtualmachines/shutdown", proxmox.PowerOffVirtualMachine)
	admin.POST("/proxmox/virtualmachines/start", proxmox.PowerOnVirtualMachine)

	// Proxmox resource monitoring endpoint
	admin.GET("/proxmox/resources", proxmox.GetProxmoxResources)

	// Proxmox Admin Pod endpoints
	admin.GET("/proxmox/pods/all", cloning.GetPods)

	// Proxmox Admin Template endpoints
	admin.POST("/proxmox/templates/publish", cloning.PublishTemplate)
	admin.POST("/proxmox/templates/update", cloning.UpdateTemplate)
	admin.GET("/proxmox/templates", cloning.GetAllTemplates)
	admin.GET("/proxmox/templates/unpublished", cloning.GetUnpublishedTemplates)
	admin.POST("/proxmox/templates/toggle", cloning.ToggleTemplateVisibility)
	admin.POST("/proxmox/templates/image/upload", images.HandleUpload)

	// Active Directory User endpoints
	admin.GET("/users", auth.GetUsers)

	// get port to run server on via. PC_PORT env variable
	port := os.Getenv("PC_PORT")
	if port == "" {
		port = "8080"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

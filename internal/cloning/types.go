package cloning

import (
	"database/sql"
	"time"

	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-gonic/gin"
)

// Config holds the configuration for cloning operations
type Config struct {
	RouterName        string        `envconfig:"ROUTER_NAME" default:"1-1NAT-pfsense"`
	RouterVMID        int           `envconfig:"ROUTER_VMID"`
	RouterNode        string        `envconfig:"ROUTER_NODE"`
	MinPodID          int           `envconfig:"MIN_POD_ID" default:"1001"`
	MaxPodID          int           `envconfig:"MAX_POD_ID" default:"1250"`
	CloneTimeout      time.Duration `envconfig:"CLONE_TIMEOUT" default:"3m"`
	RouterWaitTimeout time.Duration `envconfig:"ROUTER_WAIT_TIMEOUT" default:"120s"`
	SDNApplyTimeout   time.Duration `envconfig:"SDN_APPLY_TIMEOUT" default:"30s"`
	WANScriptPath     string        `envconfig:"WAN_SCRIPT_PATH" default:"/home/update-wan-ip.sh"`
	VIPScriptPath     string        `envconfig:"VIP_SCRIPT_PATH" default:"/home/update-wan-vip.sh"`
	WANIPBase         string        `envconfig:"WAN_IP_BASE" default:"172.16."`
	UseFullClones     bool          `envconfig:"USE_FULL_CLONES" default:"false"`
}

// KaminoTemplate represents a template in the system
type KaminoTemplate struct {
	Name            string `json:"name" binding:"required,min=1,max=100" validate:"alphanum,ascii"`
	Description     string `json:"description" binding:"required,min=1,max=500"`
	ImagePath       string `json:"image_path" binding:"omitempty,max=255" validate:"omitempty,file"`
	TemplateVisible bool   `json:"template_visible"`
	PodVisible      bool   `json:"pod_visible"`
	VMsVisible      bool   `json:"vms_visible"`
	VMCount         int    `json:"vm_count" binding:"min=0,max=100"`
	Deployments     int    `json:"deployments" binding:"min=0"`
	CreatedAt       string `json:"created_at" binding:"omitempty" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}

// DatabaseService interface defines the methods for template operations
type DatabaseService interface {
	GetTemplates() ([]KaminoTemplate, error)
	GetPublishedTemplates() ([]KaminoTemplate, error)
	InsertTemplate(template KaminoTemplate) error
	DeleteTemplate(templateName string) error
	ToggleTemplateVisibility(templateName string) error
	UploadTemplateImage(c *gin.Context) (*UploadResult, error)
	GetTemplateConfig() *TemplateConfig
	GetTemplateInfo(templateName string) (KaminoTemplate, error)
	AddDeployment(templateName string) error
	UpdateTemplate(template KaminoTemplate) error
	GetAllTemplateNames() ([]string, error)
	DeleteImage(imagePath string) error
}

// TemplateConfig holds template configuration
type TemplateConfig struct {
	UploadDir string
}

// UploadResult holds the result of a file upload
type UploadResult struct {
	Message  string `json:"message"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Path     string `json:"path"`
}

// TemplateClient implements the DatabaseService interface for template operations
type TemplateClient struct {
	DB             *sql.DB
	TemplateConfig *TemplateConfig
}

// CloningService combines Proxmox service and templates database functionality
// for handling VM cloning operations
type CloningService struct {
	ProxmoxService  proxmox.Service
	DatabaseService DatabaseService
	LDAPService     ldap.Service
	Config          *Config
}

// PodResponse represents the response structure for pod operations
type PodResponse struct {
	Pods []Pod `json:"pods"`
}

// Pod represents a pod containing VMs and template information
type Pod struct {
	Name     string                    `json:"name"`
	VMs      []proxmox.VirtualResource `json:"vms"`
	Template KaminoTemplate            `json:"template,omitempty"`
}

var allowedMIMEs = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
}

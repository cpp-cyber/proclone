package cloning

import (
	"database/sql"
	"time"

	"github.com/cpp-cyber/proclone/internal/auth"
	"github.com/cpp-cyber/proclone/internal/proxmox"
	"github.com/gin-gonic/gin"
)

// Config holds the configuration for cloning operations
type Config struct {
	Realm             string        `envconfig:"REALM"`
	RouterName        string        `envconfig:"ROUTER_NAME" default:"1-1NAT-pfsense"`
	RouterVMID        int           `envconfig:"ROUTER_VMID"`
	RouterNode        string        `envconfig:"ROUTER_NODE"`
	MinPodID          int           `envconfig:"MIN_POD_ID" default:"1001"`
	MaxPodID          int           `envconfig:"MAX_POD_ID" default:"1250"`
	CloneTimeout      time.Duration `envconfig:"CLONE_TIMEOUT" default:"3m"`
	RouterWaitTimeout time.Duration `envconfig:"ROUTER_WAIT_TIMEOUT" default:"120s"`
	SDNApplyTimeout   time.Duration `envconfig:"SDN_APPLY_TIMEOUT" default:"30s"`
	WANScriptPath     string        `envconfig:"WAN_SCRIPT_PATH" default:"/home/change-wan-ip.sh"`
	VIPScriptPath     string        `envconfig:"VIP_SCRIPT_PATH" default:"/home/change-vip-subnet.sh"`
	WANIPBase         string        `envconfig:"WAN_IP_BASE" default:"172.16."`
}

// KaminoTemplate represents a template in the system
type KaminoTemplate struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	ImagePath       string `json:"image_path"`
	TemplateVisible bool   `json:"template_visible"`
	PodVisible      bool   `json:"pod_visible"`
	VMsVisible      bool   `json:"vms_visible"`
	VMCount         int    `json:"vm_count"`
	Deployments     int    `json:"deployments"`
	CreatedAt       string `json:"created_at"`
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

// CloningManager combines Proxmox service and templates database functionality
// for handling VM cloning operations
type CloningManager struct {
	ProxmoxService  proxmox.Service
	DatabaseService DatabaseService
	LDAPService     auth.Service
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

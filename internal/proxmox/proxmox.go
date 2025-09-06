package proxmox

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
	"github.com/kelseyhightower/envconfig"
)

// ProxmoxConfig holds the configuration for Proxmox API
type ProxmoxConfig struct {
	Host         string   `envconfig:"PROXMOX_HOST" required:"true"`
	Port         string   `envconfig:"PROXMOX_PORT" default:"8006"`
	TokenID      string   `envconfig:"PROXMOX_TOKEN_ID" required:"true"`
	TokenSecret  string   `envconfig:"PROXMOX_TOKEN_SECRET" required:"true"`
	VerifySSL    bool     `envconfig:"PROXMOX_VERIFY_SSL" default:"false"`
	CriticalPool string   `envconfig:"PROXMOX_CRITICAL_POOL"`
	Realm        string   `envconfig:"REALM"`
	NodesStr     string   `envconfig:"PROXMOX_NODES"`
	Nodes        []string // Parsed from NodesStr
	APIToken     string   // Computed from TokenID and TokenSecret
}

// Service interface defines the methods for Proxmox operations
type Service interface {
	// Cluster and Resource Management
	GetClusterResourceUsage() (*ClusterResourceUsageResponse, error)
	GetClusterResources(getParams string) ([]VirtualResource, error)
	GetNodeStatus(nodeName string) (*ProxmoxNodeStatus, error)
	FindBestNode() (string, error)
	SyncUsers() error
	SyncGroups() error

	// Pod Management
	GetNextPodID(minPodID int, maxPodID int) (string, int, error)

	// VM Management
	GetVMs() ([]VirtualResource, error)
	StartVM(node string, vmID int) error
	ShutdownVM(node string, vmID int) error
	RebootVM(node string, vmID int) error
	StopVM(node string, vmID int) error
	DeleteVM(node string, vmID int) error
	ConvertVMToTemplate(node string, vmID int) error
	CloneVM(sourceVM VM, newPoolName string) (*VM, error)
	WaitForCloneCompletion(vm *VM, timeout time.Duration) error
	WaitForDisk(node string, vmid int, maxWait time.Duration) error
	WaitForRunning(vm VM) error
	WaitForStopped(vm VM) error

	// Pool Management
	GetPoolVMs(poolName string) ([]VirtualResource, error)
	CreateNewPool(poolName string) error
	SetPoolPermission(poolName string, targetName string, isGroup bool) error
	DeletePool(poolName string) error
	IsPoolEmpty(poolName string) (bool, error)
	WaitForPoolEmpty(poolName string, timeout time.Duration) error

	// Template Management
	GetTemplatePools() ([]string, error)

	// Internal access for router functionality
	GetRequestHelper() *tools.ProxmoxRequestHelper
}

// Client implements the Service interface for Proxmox operations
type Client struct {
	Config        *ProxmoxConfig
	HTTPClient    *http.Client
	BaseURL       string
	RequestHelper *tools.ProxmoxRequestHelper
}

// ProxmoxNode represents a Proxmox node
type ProxmoxNode struct {
	Node   string `json:"node"`
	Status string `json:"status"`
}

// ProxmoxNodeStatus represents the status response from a Proxmox node
type ProxmoxNodeStatus struct {
	CPU    float64 `json:"cpu"`
	Memory struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"memory"`
	Uptime int64 `json:"uptime"`
}

// NewClient creates a new Proxmox client with the given configuration
func NewClient(config ProxmoxConfig) *Client {
	// Create HTTP client with appropriate TLS settings
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !config.VerifySSL,
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	baseURL := fmt.Sprintf("https://%s:%s/api2/json", config.Host, config.Port)

	// Initialize the request helper
	requestHelper := tools.NewProxmoxRequestHelper(baseURL, config.APIToken, client)

	return &Client{
		Config:        &config,
		HTTPClient:    client,
		BaseURL:       baseURL,
		RequestHelper: requestHelper,
	}
}

// NewService creates a new Proxmox service, loading configuration internally
func NewService() (Service, error) {
	config, err := LoadProxmoxConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load Proxmox configuration: %w", err)
	}

	return NewClient(*config), nil
}

// GetRequestHelper returns the Proxmox request helper for internal use
func (c *Client) GetRequestHelper() *tools.ProxmoxRequestHelper {
	return c.RequestHelper
}

// LoadProxmoxConfig loads and validates Proxmox configuration from environment variables
func LoadProxmoxConfig() (*ProxmoxConfig, error) {
	var config ProxmoxConfig
	if err := envconfig.Process("", &config); err != nil {
		return nil, fmt.Errorf("failed to process Proxmox configuration: %w", err)
	}

	// Build API token from ID and secret
	config.APIToken = fmt.Sprintf("%s=%s", config.TokenID, config.TokenSecret)

	// Parse nodes list if provided
	if config.NodesStr != "" {
		config.Nodes = strings.Split(config.NodesStr, ",")
		// Trim whitespace from each node
		for i, node := range config.Nodes {
			config.Nodes[i] = strings.TrimSpace(node)
		}
	}

	return &config, nil
}

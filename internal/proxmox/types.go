package proxmox

import (
	"net/http"
	"time"

	"github.com/cpp-cyber/proclone/internal/tools"
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
	StorageID    string   `envconfig:"STORAGE_ID" default:"local-lvm"`
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
	GetNextPodIDs(minPodID int, maxPodID int, num int) ([]string, []int, error)

	// VM Management
	GetVMs() ([]VirtualResource, error)
	GetNextVMIDs(num int) ([]int, error)
	StartVM(node string, vmID int) error
	ShutdownVM(node string, vmID int) error
	RebootVM(node string, vmID int) error
	StopVM(node string, vmID int) error
	DeleteVM(node string, vmID int) error
	GetVMSnapshots(node string, vmID int) ([]VMSnapshot, error)
	DeleteVMSnapshot(node string, vmID int, snapshotName string) error
	ConvertVMToTemplate(node string, vmID int) error
	CloneVM(req VMCloneRequest) error
	WaitForDisk(node string, vmID int, maxWait time.Duration) error
	WaitForRunning(node string, vmiID int) error
	WaitForStopped(node string, vmiID int) error

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

// ProxmoxService implements the Service interface for Proxmox operations
type ProxmoxService struct {
	Config        *ProxmoxConfig
	HTTPClient    *http.Client
	BaseURL       string
	RequestHelper *tools.ProxmoxRequestHelper
}

type ProxmoxNode struct {
	Node   string `json:"node"`
	Status string `json:"status"`
}

type ProxmoxNodeStatus struct {
	CPU    float64 `json:"cpu"`
	Memory struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"memory"`
	Uptime int64 `json:"uptime"`
}

type VirtualResourceConfig struct {
	HardDisk string `json:"scsi0"`
	Lock     string `json:"lock,omitempty"`
	Net0     string `json:"net0"`
	Net1     string `json:"net1,omitempty"`
}

type VirtualResourceStatus struct {
	Status string `json:"status"`
}

type VNetResponse []struct {
	VNet string `json:"vnet"`
}

type VM struct {
	Name string `json:"name,omitempty"`
	Node string `json:"node"`
	VMID int    `json:"vmid"`
}

type VMCloneRequest struct {
	SourceVM   VM
	PoolName   string
	PodID      string
	NewVMID    int
	Full       int
	TargetNode string
}

type VMSnapshot struct {
	Name string `json:"name"`
}

type VirtualResource struct {
	CPU           float64 `json:"cpu,omitempty"`
	MaxCPU        int     `json:"maxcpu,omitempty"`
	Mem           int     `json:"mem,omitempty"`
	MaxMem        int     `json:"maxmem,omitempty"`
	Type          string  `json:"type,omitempty"`
	Id            string  `json:"id,omitempty"`
	Name          string  `json:"name,omitempty"`
	NodeName      string  `json:"node,omitempty"`
	ResourcePool  string  `json:"pool,omitempty"`
	RunningStatus string  `json:"status,omitempty"`
	Uptime        int     `json:"uptime,omitempty"`
	VmId          int     `json:"vmid,omitempty"`
	Storage       string  `json:"storage,omitempty"`
	Disk          int64   `json:"disk,omitempty"`
	MaxDisk       int64   `json:"maxdisk,omitempty"`
	Template      int     `json:"template,omitempty"`
}

type ResourceUsage struct {
	CPUUsage     float64 `json:"cpu_usage"`     // CPU usage percentage
	MemoryUsed   int64   `json:"memory_used"`   // Used memory in bytes
	MemoryTotal  int64   `json:"memory_total"`  // Total memory in bytes
	StorageUsed  int64   `json:"storage_used"`  // Used storage in bytes
	StorageTotal int64   `json:"storage_total"` // Total storage in bytes
}

type NodeResourceUsage struct {
	Name      string        `json:"name"`
	Resources ResourceUsage `json:"resources"`
}

type ClusterResourceUsageResponse struct {
	Total  ResourceUsage       `json:"total"`
	Nodes  []NodeResourceUsage `json:"nodes"`
	Errors []string            `json:"errors,omitempty"`
}

type PendingDiskResponse struct {
	Used int64 `json:"used"`
	Size int64 `json:"size"`
}

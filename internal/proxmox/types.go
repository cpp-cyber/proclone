package proxmox

// ConfigResponse represents VM configuration response
type ConfigResponse struct {
	HardDisk string `json:"scsi0,omitempty"`
	Lock     string `json:"lock,omitempty"`
}

// VNetResponse represents the VNet API response
type VNetResponse []struct {
	VNet string `json:"vnet"`
}

// VM represents a Virtual Machine with node and ID information
type VM struct {
	Name string `json:"name,omitempty"`
	Node string `json:"node"`
	VMID int    `json:"vmid"`
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

// NodeResourceUsage represents the resource usage metrics for a single node
type NodeResourceUsage struct {
	Name      string        `json:"name"`
	Resources ResourceUsage `json:"resources"`
}

// ResourceUsageResponse represents the API response containing resource usage for all nodes
type ClusterResourceUsageResponse struct {
	Total  ResourceUsage       `json:"total"`
	Nodes  []NodeResourceUsage `json:"nodes"`
	Errors []string            `json:"errors,omitempty"`
}

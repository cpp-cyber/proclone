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

// NewProxmoxService creates a new Proxmox service with the given configuration
func NewProxmoxService(config ProxmoxConfig) *ProxmoxService {
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

	return &ProxmoxService{
		Config:        &config,
		HTTPClient:    client,
		BaseURL:       baseURL,
		RequestHelper: requestHelper,
	}
}

func NewService() (Service, error) {
	config, err := LoadProxmoxConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load Proxmox configuration: %w", err)
	}

	return NewProxmoxService(*config), nil
}

func (s *ProxmoxService) GetRequestHelper() *tools.ProxmoxRequestHelper {
	return s.RequestHelper
}

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

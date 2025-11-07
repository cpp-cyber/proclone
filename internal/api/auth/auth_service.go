package auth

import (
	"fmt"
	"slices"

	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
)

func NewAuthService(proxmoxService *proxmox.ProxmoxService) (*AuthService, error) {
	ldapService, err := ldap.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	return &AuthService{
		ldapService:    ldapService,
		proxmoxService: proxmoxService,
	}, nil
}

func (s *AuthService) Authenticate(username string, password string) (bool, error) {
	// Input validation
	if username == "" || password == "" {
		return false, nil // Invalid credentials, not an error
	}

	// Get user DN first to validate user exists
	userDN, err := s.ldapService.GetUserDN(username)
	if err != nil {
		return false, nil // User not found, not an error for security reasons
	}

	// Create a temporary client for authentication to avoid privilege escalation
	config, err := ldap.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load LDAP config: %w", err)
	}

	authClient := ldap.NewClient(config)
	if err := authClient.Connect(); err != nil {
		return false, fmt.Errorf("failed to connect to LDAP: %w", err)
	}
	defer authClient.Disconnect()

	// Try to bind as the user to verify password
	if err := authClient.SimpleBind(userDN, password); err != nil {
		return false, nil // Invalid credentials, not an error
	}

	return true, nil
}

func (s *AuthService) IsAdmin(username string) (bool, error) {
	// Get user's groups from Proxmox
	userGroups, err := s.proxmoxService.GetUserGroups(username)
	if err != nil {
		return false, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Get the admin group name from config
	adminGroupName := s.proxmoxService.Config.AdminGroupName

	// Check if user is in the admin group
	if slices.Contains(userGroups, adminGroupName) {
		return true, nil
	}

	return false, nil
}

func (s *AuthService) HealthCheck() error {
	return s.ldapService.HealthCheck()
}

func (s *AuthService) Reconnect() error {
	return s.ldapService.Reconnect()
}

package auth

import (
	"fmt"
	"strings"

	"github.com/cpp-cyber/proclone/internal/ldap"
)

func NewAuthService() (*AuthService, error) {
	ldapService, err := ldap.NewLDAPService()
	if err != nil {
		return nil, fmt.Errorf("failed to create LDAP service: %w", err)
	}

	return &AuthService{
		ldapService: ldapService,
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
	// Input validation
	if username == "" {
		return false, fmt.Errorf("username cannot be empty")
	}

	// Get user DN
	userDN, err := s.ldapService.GetUserDN(username)
	if err != nil {
		return false, fmt.Errorf("failed to get user DN: %w", err)
	}

	// Get user's groups
	userGroups, err := s.ldapService.GetUserGroups(userDN)
	if err != nil {
		return false, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Load LDAP config to get admin group DN
	config, err := ldap.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load LDAP config: %w", err)
	}

	if config.AdminGroupDN == "" {
		return false, fmt.Errorf("admin group DN not configured")
	}

	// Check if user is in the admin group
	for _, groupDN := range userGroups {
		if strings.EqualFold(groupDN, "Proxmox-Admins") {
			return true, nil
		}
	}

	return false, nil
}

func (s *AuthService) HealthCheck() error {
	return s.ldapService.HealthCheck()
}

func (s *AuthService) Reconnect() error {
	return s.ldapService.Reconnect()
}

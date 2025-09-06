package auth

import (
	"fmt"
	"strings"

	"github.com/cpp-cyber/proclone/internal/ldap"
	ldapv3 "github.com/go-ldap/ldap/v3"
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
	// Get the LDAP service to perform authentication
	ldapSvc, ok := s.ldapService.(*ldap.LDAPService)
	if !ok {
		return false, fmt.Errorf("invalid LDAP service type")
	}

	userDN, err := ldapSvc.GetUserDN(username)
	if err != nil {
		return false, fmt.Errorf("failed to get user DN: %v", err)
	}

	// Create a temporary client for authentication
	config, err := ldap.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load LDAP config: %v", err)
	}

	authClient := ldap.NewClient(config)
	err = authClient.Connect()
	if err != nil {
		return false, fmt.Errorf("failed to connect to LDAP: %v", err)
	}
	defer authClient.Disconnect()

	// Try to bind as the user to verify password
	err = authClient.Bind(userDN, password)
	if err != nil {
		return false, nil // Invalid credentials, not an error
	}

	return true, nil
}

func (s *AuthService) IsAdmin(username string) (bool, error) {
	config, err := ldap.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load LDAP config: %v", err)
	}

	// Create a client for admin check
	client := ldap.NewClient(config)
	err = client.Connect()
	if err != nil {
		return false, fmt.Errorf("failed to connect to LDAP: %v", err)
	}
	defer client.Disconnect()

	// Search for admin group
	adminGroupReq := ldapv3.NewSearchRequest(
		config.AdminGroupDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		"(objectClass=group)",
		[]string{"member"},
		nil,
	)

	// Search for user DN
	userDNReq := ldapv3.NewSearchRequest(
		config.BaseDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(uid=%s))", ldapv3.EscapeFilter(username)),
		[]string{"dn"},
		nil,
	)

	adminGroupResult, err := client.Search(adminGroupReq)
	if err != nil {
		return false, fmt.Errorf("failed to search admin group: %v", err)
	}

	userResult, err := client.Search(userDNReq)
	if err != nil {
		return false, fmt.Errorf("failed to search user: %v", err)
	}

	if len(adminGroupResult.Entries) == 0 {
		return false, fmt.Errorf("admin group not found")
	}

	if len(userResult.Entries) == 0 {
		return false, fmt.Errorf("user not found")
	}

	adminMembers := adminGroupResult.Entries[0].GetAttributeValues("member")
	userDN := userResult.Entries[0].DN

	// Check if user DN is in admin group members
	for _, member := range adminMembers {
		if strings.EqualFold(member, userDN) {
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

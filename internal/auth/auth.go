package auth

import (
	"fmt"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// Service defines the authentication service interface
type Service interface {
	Authenticate(username, password string) (bool, error)
	IsAdmin(username string) (bool, error)
	GetUsers() ([]User, error)
	CreateAndRegisterUser(userInfo UserRegistrationInfo) error
	DeleteUser(username string) error
	AddUserToGroup(username string, groupName string) error
	SetUserGroups(username string, groups []string) error
	CreateGroup(groupName string) error
	GetGroups() ([]Group, error)
	RenameGroup(oldGroupName string, newGroupName string) error
	DeleteGroup(groupName string) error
	GetGroupMembers(groupName string) ([]User, error)
	RemoveUserFromGroup(username string, groupName string) error
	EnableUserAccount(username string) error
	DisableUserAccount(username string) error
	HealthCheck() error
	Reconnect() error
	AddUsersToGroup(groupName string, usernames []string) error
	RemoveUsersFromGroup(groupName string, usernames []string) error
	GetUserGroups(userDN string) ([]string, error)
	GetUserDN(username string) (string, error)
}

// LDAPService implements authentication using LDAP
type LDAPService struct {
	client *Client
}

// NewLDAPService creates a new LDAP authentication service
func NewLDAPService() (*LDAPService, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load LDAP configuration: %w", err)
	}

	client := NewClient(config)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}

	return &LDAPService{
		client: client,
	}, nil
}

// Authenticate performs user authentication against LDAP
func (s *LDAPService) Authenticate(username, password string) (bool, error) {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return false, fmt.Errorf("failed to get user DN: %v", err)
	}

	// Bind as user to verify password
	err = s.client.Bind(userDN, password)
	if err != nil {
		return false, nil // Invalid credentials, not an error
	}

	// Rebind as service account for further operations
	config := s.client.Config()
	if config.BindUser != "" {
		err = s.client.Bind(config.BindUser, config.BindPassword)
		if err != nil {
			return false, fmt.Errorf("failed to rebind as service account: %v", err)
		}
	}

	return true, nil
}

// IsAdmin checks if a user is a member of the admin group
func (s *LDAPService) IsAdmin(username string) (bool, error) {
	config := s.client.Config()

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
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", username),
		[]string{"dn"},
		nil,
	)

	adminGroupEntry, err := s.client.SearchEntry(adminGroupReq)
	if err != nil {
		return false, fmt.Errorf("failed to search admin group: %v", err)
	}

	userEntry, err := s.client.SearchEntry(userDNReq)
	if err != nil {
		return false, fmt.Errorf("failed to search user: %v", err)
	}

	if adminGroupEntry == nil {
		return false, fmt.Errorf("admin group not found")
	}

	if userEntry == nil {
		return false, fmt.Errorf("user not found")
	}

	// Check if user DN is in admin group members
	for _, member := range adminGroupEntry.GetAttributeValues("member") {
		if member == userEntry.DN {
			return true, nil
		}
	}

	return false, nil
}

// Close closes the LDAP connection
func (s *LDAPService) Close() error {
	return s.client.Disconnect()
}

// HealthCheck verifies that the LDAP connection is working
func (s *LDAPService) HealthCheck() error {
	return s.client.HealthCheck()
}

// Reconnect attempts to reconnect to the LDAP server
func (s *LDAPService) Reconnect() error {
	return s.client.Connect()
}

// SetPassword sets the password for a user using User struct
func (s *LDAPService) SetPassword(user User, password string) error {
	userDN, err := s.GetUserDN(user.Name)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	return s.SetUserPassword(userDN, password)
}

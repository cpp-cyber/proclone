package auth

import (
	"fmt"
	"log"

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
	log.Println("[DEBUG] NewLDAPService: Starting LDAP service initialization")

	config, err := LoadConfig()
	if err != nil {
		log.Printf("[ERROR] NewLDAPService: Failed to load LDAP configuration: %v", err)
		return nil, fmt.Errorf("failed to load LDAP configuration: %w", err)
	}
	log.Printf("[DEBUG] NewLDAPService: LDAP configuration loaded successfully - URL: %s, BindUser: %s", config.URL, config.BindUser)

	client := NewClient(config)
	if err := client.Connect(); err != nil {
		log.Printf("[ERROR] NewLDAPService: Failed to connect to LDAP: %v", err)
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}
	log.Println("[DEBUG] NewLDAPService: LDAP client connected successfully")

	log.Println("[INFO] NewLDAPService: LDAP service initialized successfully")
	return &LDAPService{
		client: client,
	}, nil
}

// Authenticate performs user authentication against LDAP
func (s *LDAPService) Authenticate(username, password string) (bool, error) {
	log.Printf("[DEBUG] Authenticate: Starting authentication for user: %s", username)

	userDN, err := s.GetUserDN(username)
	if err != nil {
		log.Printf("[ERROR] Authenticate: Failed to get user DN for %s: %v", username, err)
		return false, fmt.Errorf("failed to get user DN: %v", err)
	}
	log.Printf("[DEBUG] Authenticate: Retrieved user DN for %s: %s", username, userDN)

	// Bind as user to verify password
	log.Printf("[DEBUG] Authenticate: Attempting to bind as user: %s", username)
	err = s.client.Bind(userDN, password)
	if err != nil {
		log.Printf("[WARN] Authenticate: Authentication failed for user %s: %v", username, err)
		return false, nil // Invalid credentials, not an error
	}
	log.Printf("[DEBUG] Authenticate: User bind successful for: %s", username)

	// Rebind as service account for further operations
	config := s.client.Config()
	if config.BindUser != "" {
		log.Printf("[DEBUG] Authenticate: Rebinding as service account: %s", config.BindUser)
		err = s.client.Bind(config.BindUser, config.BindPassword)
		if err != nil {
			log.Printf("[ERROR] Authenticate: Failed to rebind as service account: %v", err)
			return false, fmt.Errorf("failed to rebind as service account: %v", err)
		}
		log.Println("[DEBUG] Authenticate: Service account rebind successful")
	}

	log.Printf("[INFO] Authenticate: Authentication successful for user: %s", username)
	return true, nil
}

// IsAdmin checks if a user is a member of the admin group
func (s *LDAPService) IsAdmin(username string) (bool, error) {
	log.Printf("[DEBUG] IsAdmin: Checking admin status for user: %s", username)
	config := s.client.Config()

	// Search for admin group
	log.Printf("[DEBUG] IsAdmin: Searching for admin group: %s", config.AdminGroupDN)
	adminGroupReq := ldapv3.NewSearchRequest(
		config.AdminGroupDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		"(objectClass=group)",
		[]string{"member"},
		nil,
	)

	// Search for user DN
	log.Printf("[DEBUG] IsAdmin: Searching for user DN for: %s", username)
	userDNReq := ldapv3.NewSearchRequest(
		config.BaseDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", username),
		[]string{"dn"},
		nil,
	)

	adminGroupEntry, err := s.client.SearchEntry(adminGroupReq)
	if err != nil {
		log.Printf("[ERROR] IsAdmin: Failed to search admin group: %v", err)
		return false, fmt.Errorf("failed to search admin group: %v", err)
	}

	userEntry, err := s.client.SearchEntry(userDNReq)
	if err != nil {
		log.Printf("[ERROR] IsAdmin: Failed to search user %s: %v", username, err)
		return false, fmt.Errorf("failed to search user: %v", err)
	}

	if adminGroupEntry == nil {
		log.Printf("[ERROR] IsAdmin: Admin group not found: %s", config.AdminGroupDN)
		return false, fmt.Errorf("admin group not found")
	}

	if userEntry == nil {
		log.Printf("[ERROR] IsAdmin: User not found: %s", username)
		return false, fmt.Errorf("user not found")
	}

	log.Printf("[DEBUG] IsAdmin: User DN found: %s", userEntry.DN)
	adminMembers := adminGroupEntry.GetAttributeValues("member")
	log.Printf("[DEBUG] IsAdmin: Admin group has %d members", len(adminMembers))

	// Check if user DN is in admin group members
	for _, member := range adminMembers {
		if member == userEntry.DN {
			log.Printf("[INFO] IsAdmin: User %s is an admin", username)
			return true, nil
		}
	}

	log.Printf("[DEBUG] IsAdmin: User %s is not an admin", username)
	return false, nil
}

// Close closes the LDAP connection
func (s *LDAPService) Close() error {
	log.Println("[DEBUG] Close: Closing LDAP connection")
	err := s.client.Disconnect()
	if err != nil {
		log.Printf("[ERROR] Close: Failed to close LDAP connection: %v", err)
	} else {
		log.Println("[INFO] Close: LDAP connection closed successfully")
	}
	return err
}

// HealthCheck verifies that the LDAP connection is working
func (s *LDAPService) HealthCheck() error {
	log.Println("[DEBUG] HealthCheck: Performing LDAP health check")
	err := s.client.HealthCheck()
	if err != nil {
		log.Printf("[ERROR] HealthCheck: LDAP health check failed: %v", err)
	} else {
		log.Println("[DEBUG] HealthCheck: LDAP health check passed")
	}
	return err
}

// Reconnect attempts to reconnect to the LDAP server
func (s *LDAPService) Reconnect() error {
	log.Println("[DEBUG] Reconnect: Attempting to reconnect to LDAP server")
	err := s.client.Connect()
	if err != nil {
		log.Printf("[ERROR] Reconnect: Failed to reconnect to LDAP server: %v", err)
	} else {
		log.Println("[INFO] Reconnect: Successfully reconnected to LDAP server")
	}
	return err
}

// SetPassword sets the password for a user using User struct
func (s *LDAPService) SetPassword(user User, password string) error {
	log.Printf("[DEBUG] SetPassword: Setting password for user: %s", user.Name)
	userDN, err := s.GetUserDN(user.Name)
	if err != nil {
		log.Printf("[ERROR] SetPassword: Failed to get user DN for %s: %v", user.Name, err)
		return fmt.Errorf("failed to get user DN: %v", err)
	}
	log.Printf("[DEBUG] SetPassword: Retrieved user DN: %s", userDN)

	err = s.SetUserPassword(userDN, password)
	if err != nil {
		log.Printf("[ERROR] SetPassword: Failed to set password for user %s: %v", user.Name, err)
	} else {
		log.Printf("[INFO] SetPassword: Password set successfully for user: %s", user.Name)
	}
	return err
}

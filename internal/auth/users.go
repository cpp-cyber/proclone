package auth

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

type User struct {
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	Enabled   bool    `json:"enabled"`
	IsAdmin   bool    `json:"is_admin"`
	Groups    []Group `json:"groups"`
}

// UserRegistrationInfo contains the information needed to register a new user
type UserRegistrationInfo struct {
	Username string `json:"username" validate:"required,min=1,max=20"`
	Password string `json:"password" validate:"required,min=8"`
}

// NewUserRegistrationInfo creates a new UserRegistrationInfo with required fields
func NewUserRegistrationInfo(username, password string) *UserRegistrationInfo {
	return &UserRegistrationInfo{
		Username: username,
		Password: password,
	}
}

// Validate validates the user registration information
func (u *UserRegistrationInfo) Validate() error {
	if err := validateUsername(u.Username); err != nil {
		return err
	}

	if err := validatePasswordReq(u.Password); err != nil {
		return err
	}

	return nil
}

// GetUsers retrieves all users from LDAP
func (s *LDAPService) GetUsers() ([]User, error) {
	config := s.client.Config()

	// Create search request to find all user objects who are members of KaminoUsers group
	kaminoUsersGroupDN := "CN=KaminoUsers,OU=KaminoGroups," + config.BaseDN
	req := ldapv3.NewSearchRequest(
		config.BaseDN, ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=*)(memberOf=%s))", kaminoUsersGroupDN), // Filter for users in KaminoUsers group
		[]string{"sAMAccountName", "dn", "whenCreated", "memberOf", "userAccountControl"},       // Attributes to retrieve
		nil,
	)

	// Perform the search
	searchResult, err := s.client.Search(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for users: %v", err)
	}

	var users = make([]User, 0)
	for _, entry := range searchResult.Entries {
		user := User{
			Name: entry.GetAttributeValue("sAMAccountName"),
		}

		// Add creation date if available and convert it
		whenCreated := entry.GetAttributeValue("whenCreated")
		if whenCreated != "" {
			// AD stores dates in GeneralizedTime format: YYYYMMDDHHMMSS.0Z
			if parsedTime, err := time.Parse("20060102150405.0Z", whenCreated); err == nil {
				user.CreatedAt = parsedTime.Format("2006-01-02 15:04:05")
			}
		}

		// Check if user is enabled by parsing userAccountControl
		user.Enabled = isUserEnabled(entry.GetAttributeValue("userAccountControl"))

		// Check for admin privileges and add group memberships
		var groups []Group
		var isAdmin = false
		for _, groupDN := range entry.GetAttributeValues("memberOf") {
			// Check if user is admin based on group membership
			groupName := extractCNFromDN(groupDN)
			if groupName == "Domain Admins" || groupName == "Proxmox-Admins" {
				isAdmin = true
			}

			// Only include groups from Kamino-Groups OU in the groups list
			if !strings.Contains(strings.ToLower(groupDN), "ou=kaminogroups") {
				continue
			}

			// Add group to user's groups list
			if groupName != "" {
				groups = append(groups, Group{
					Name: groupName,
				})
			}
		}

		user.IsAdmin = isAdmin
		user.Groups = groups

		users = append(users, user)
	}

	return users, nil
}

// extractDomainFromDN extracts the domain name from a Distinguished Name
func extractDomainFromDN(dn string) string {
	// Convert DN like "DC=example,DC=com" to "example.com"
	parts := strings.Split(strings.ToLower(dn), ",")
	var domainParts []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "dc=") {
			domainParts = append(domainParts, strings.TrimPrefix(part, "dc="))
		}
	}

	return strings.Join(domainParts, ".")
}

// encodePasswordForAD encodes a password for Active Directory unicodePwd attribute
func encodePasswordForAD(password string) string {
	// AD requires password to be UTF-16LE encoded and surrounded by quotes
	quotedPassword := fmt.Sprintf("\"%s\"", password)
	utf16Encoded := utf16.Encode([]rune(quotedPassword))

	// Convert to bytes in little-endian format
	bytes := make([]byte, len(utf16Encoded)*2)
	for i, r := range utf16Encoded {
		binary.LittleEndian.PutUint16(bytes[i*2:], r)
	}

	return string(bytes)
}

// validateUsername validates a username according to Active Directory requirements
func validateUsername(username string) error {
	if len(username) < 1 || len(username) > 20 {
		return fmt.Errorf("username must be between 1 and 20 characters")
	}

	regex := regexp.MustCompile("^[a-zA-Z0-9]*$")
	if !regex.MatchString(username) {
		return fmt.Errorf("username must only contain letters and numbers")
	}

	return nil
}

// validatePasswordReq validates a password according to requirements
func validatePasswordReq(password string) error {
	var number, letter bool
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}

	if len(password) > 128 {
		return fmt.Errorf("password must not exceed 128 characters")
	}

	for _, c := range password {
		switch {
		case unicode.IsNumber(c):
			number = true
		case unicode.IsLetter(c):
			letter = true
		}
	}

	if !number || !letter {
		return fmt.Errorf("password must contain at least one letter and one number")
	}

	return nil
}

// extractCNFromDN extracts the Common Name (CN) from a Distinguished Name (DN)
func extractCNFromDN(dn string) string {
	// Split the DN by commas and look for the CN component
	parts := strings.Split(dn, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "cn=") {
			// Extract the value after "CN="
			return strings.TrimPrefix(part, strings.Split(part, "=")[0]+"=")
		}
	}
	return ""
}

// isUserEnabled checks if a user is enabled based on userAccountControl attribute
// In Active Directory, userAccountControl is a bitmask where bit 1 (value 2) indicates "ACCOUNTDISABLE"
// If this bit is set, the account is disabled
func isUserEnabled(userAccountControl string) bool {
	if userAccountControl == "" {
		return false // Default to disabled if attribute is missing
	}

	// Parse the userAccountControl value
	uac, err := strconv.ParseInt(userAccountControl, 10, 64)
	if err != nil {
		return false // Default to disabled if parsing fails
	}

	// Check if the ACCOUNTDISABLE bit (0x2) is set
	// If bit 1 is set, the account is disabled, so we return the inverse
	const ACCOUNTDISABLE = 0x2
	return (uac & ACCOUNTDISABLE) == 0
}

// CreateUserWithInfo creates a new user in LDAP with detailed information
func (s *LDAPService) CreateUser(userInfo UserRegistrationInfo) (string, error) {
	config := s.client.Config()

	// Create DN for new user in Users container
	// TODO: Static
	userDN := fmt.Sprintf("CN=%s,OU=KaminoUsers,%s", userInfo.Username, config.BaseDN)

	// Create add request for new user
	addReq := ldapv3.NewAddRequest(userDN, nil)

	// Add required object classes
	addReq.Attribute("objectClass", []string{"top", "person", "organizationalPerson", "user"})

	// Add basic attributes
	addReq.Attribute("cn", []string{userInfo.Username})
	addReq.Attribute("sAMAccountName", []string{userInfo.Username})
	addReq.Attribute("userPrincipalName", []string{fmt.Sprintf("%s@%s", userInfo.Username, extractDomainFromDN(config.BaseDN))})

	// Set account control flags - account disabled initially (will be enabled after password is set)
	addReq.Attribute("userAccountControl", []string{"546"}) // NORMAL_ACCOUNT + ACCOUNTDISABLE

	// Perform the add operation
	err := s.client.Add(addReq)
	if err != nil {
		return "", fmt.Errorf("failed to create user: %v", err)
	}

	return userDN, nil
}

// SetUserPassword sets the password for a user by DN
func (s *LDAPService) SetUserPassword(userDN string, password string) error {
	// For Active Directory, passwords must be set using unicodePwd attribute
	// The password must be UTF-16LE encoded and quoted
	utf16Password := encodePasswordForAD(password)

	// Create modify request to set password
	modifyReq := ldapv3.NewModifyRequest(userDN, nil)
	modifyReq.Replace("unicodePwd", []string{utf16Password})

	err := s.client.Modify(modifyReq)
	if err != nil {
		return fmt.Errorf("failed to set password: %v", err)
	}

	return nil
}

// EnableUserAccountByDN enables a user account by updating userAccountControl
func (s *LDAPService) EnableUserAccountByDN(userDN string) error {
	// Set userAccountControl to 512 (NORMAL_ACCOUNT) to enable the account
	modifyReq := ldapv3.NewModifyRequest(userDN, nil)
	modifyReq.Replace("userAccountControl", []string{"512"})

	err := s.client.Modify(modifyReq)
	if err != nil {
		return fmt.Errorf("failed to enable account: %v", err)
	}

	return nil
}

// DisableUserAccountByDN disables a user account by updating userAccountControl
func (s *LDAPService) DisableUserAccountByDN(userDN string) error {
	// Set userAccountControl to 546 (NORMAL_ACCOUNT + ACCOUNTDISABLE) to disable the account
	modifyReq := ldapv3.NewModifyRequest(userDN, nil)
	modifyReq.Replace("userAccountControl", []string{"546"})

	err := s.client.Modify(modifyReq)
	if err != nil {
		return fmt.Errorf("failed to disable account: %v", err)
	}

	return nil
}

// AddToGroup adds a user to a group by DN
func (s *LDAPService) AddToGroup(userDN string, groupDN string) error {
	// Create modify request to add user to group
	modifyReq := ldapv3.NewModifyRequest(groupDN, nil)
	modifyReq.Add("member", []string{userDN})

	err := s.client.Modify(modifyReq)
	if err != nil {
		// Check if the error is because the user is already in the group
		if strings.Contains(strings.ToLower(err.Error()), "already exists") ||
			strings.Contains(strings.ToLower(err.Error()), "attribute or value exists") {
			return nil // Not an error if user is already in group
		}
		return fmt.Errorf("failed to add user to group: %v", err)
	}

	return nil
}

// RegisterUser creates, configures, and enables a new user account
func (s *LDAPService) CreateAndRegisterUser(userInfo UserRegistrationInfo) error {
	// Validate username and password
	if err := userInfo.Validate(); err != nil {
		return err
	}

	// Create the user with full information
	userDN, err := s.CreateUser(userInfo)
	if err != nil {
		return fmt.Errorf("failed to create user: %v", err)
	}

	// Set the password
	err = s.SetUserPassword(userDN, userInfo.Password)
	if err != nil {
		return fmt.Errorf("failed to set password: %v", err)
	}

	// Add user to default user group
	config := s.client.Config()
	userGroupDN := fmt.Sprintf("CN=KaminoUsers,OU=KaminoGroups,%s", config.BaseDN)
	err = s.AddToGroup(userDN, userGroupDN)
	if err != nil {
		return fmt.Errorf("failed to add user to group: %v", err)
	}

	// Enable the account
	err = s.EnableUserAccountByDN(userDN)
	if err != nil {
		return fmt.Errorf("failed to enable account: %v", err)
	}

	return nil
}

// AddUserToGroup adds a user to a group in LDAP by names
func (s *LDAPService) AddUserToGroup(username string, groupName string) error {
	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Get group DN
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group %s: %v", groupName, err)
	}

	// Create modify request to add user to group
	modifyReq := ldapv3.NewModifyRequest(groupDN, nil)
	modifyReq.Add("member", []string{userDN})

	err = s.client.Modify(modifyReq)
	if err != nil {
		// Check if the error is because the user is already in the group
		if strings.Contains(strings.ToLower(err.Error()), "already exists") ||
			strings.Contains(strings.ToLower(err.Error()), "attribute or value exists") {
			return fmt.Errorf("user %s is already a member of group %s", username, groupName)
		}
		return fmt.Errorf("failed to add user %s to group %s: %v", username, groupName, err)
	}

	return nil
}

// RemoveUserFromGroup removes a user from a group in LDAP
func (s *LDAPService) RemoveUserFromGroup(username string, groupName string) error {
	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Get group DN
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group %s: %v", groupName, err)
	}

	// Create modify request to remove user from group
	modifyReq := ldapv3.NewModifyRequest(groupDN, nil)
	modifyReq.Delete("member", []string{userDN})

	err = s.client.Modify(modifyReq)
	if err != nil {
		// Check if the error is because the user is not in the group
		if strings.Contains(strings.ToLower(err.Error()), "no such attribute") ||
			strings.Contains(strings.ToLower(err.Error()), "no such value") {
			return fmt.Errorf("user %s is not a member of group %s", username, groupName)
		}
		return fmt.Errorf("failed to remove user %s from group %s: %v", username, groupName, err)
	}

	return nil
}

func (s *LDAPService) DeleteUser(username string) error {
	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Verify that the user is not an admin
	userGroups, err := s.GetUserGroups(userDN)
	if err != nil {
		return fmt.Errorf("failed to get user groups: %v", err)
	}

	isAdmin, err := isAdmin(userGroups)
	if err != nil {
		return fmt.Errorf("failed to check if user is admin: %v", err)
	}
	if isAdmin {
		return fmt.Errorf("cannot delete admin user %s", username)
	}

	// Create delete request
	delReq := ldapv3.NewDelRequest(userDN, nil)

	err = s.client.Delete(delReq)
	if err != nil {
		return fmt.Errorf("failed to delete user %s: %v", username, err)
	}

	return nil
}

func (s *LDAPService) DeleteUsers(usernames []string) []error {
	var errors []error
	var validUsers []string
	var userDNs []string

	// First pass: validate all users and collect their DNs
	for _, username := range usernames {
		// Get user DN
		userDN, err := s.GetUserDN(username)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to find user %s: %v", username, err))
			continue
		}

		// Verify that the user is not an admin
		userGroups, err := s.GetUserGroups(userDN)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to get user groups for %s: %v", username, err))
			continue
		}

		isAdmin, err := isAdmin(userGroups)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to check if user %s is admin: %v", username, err))
			continue
		}
		if isAdmin {
			errors = append(errors, fmt.Errorf("cannot delete admin user %s", username))
			continue
		}

		// User passed validation
		validUsers = append(validUsers, username)
		userDNs = append(userDNs, userDN)
	}

	// Second pass: delete all valid users
	for i, userDN := range userDNs {
		username := validUsers[i]
		delReq := ldapv3.NewDelRequest(userDN, nil)

		err := s.client.Delete(delReq)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to delete user %s: %v", username, err))
		}
	}

	return errors
}

func (s *LDAPService) GetUserGroups(userDN string) ([]string, error) {
	config := s.client.Config()

	// Search for groups that the user is a member of (search entire base DN to find all groups including admin groups)
	groupSearchReq := ldapv3.NewSearchRequest(
		config.BaseDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=group)(member=%s))", userDN),
		[]string{"cn", "distinguishedName"},
		nil,
	)

	groupEntries, err := s.client.Search(groupSearchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to search user groups: %v", err)
	}

	var groups []string
	for _, entry := range groupEntries.Entries {
		groupName := entry.GetAttributeValue("cn")
		if groupName != "" {
			groups = append(groups, groupName)
		}
	}

	return groups, nil
}

func isAdmin(groups []string) (bool, error) {
	// If groups is empty, user is not an admin
	if len(groups) == 0 {
		return false, nil
	}

	for _, group := range groups {
		group = strings.ToLower(group)
		if strings.Contains(group, "admins") || strings.Contains(group, "proxmox-admins") {
			return true, nil
		}
	}

	return false, nil
}

// EnableUserAccount enables a user account by username (wrapper method for Service interface)
func (s *LDAPService) EnableUserAccount(username string) error {
	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Set userAccountControl to 512 (NORMAL_ACCOUNT) to enable the account
	modifyReq := ldapv3.NewModifyRequest(userDN, nil)
	modifyReq.Replace("userAccountControl", []string{"512"})

	err = s.client.Modify(modifyReq)
	if err != nil {
		return fmt.Errorf("failed to enable account for user %s: %v", username, err)
	}

	return nil
}

// DisableUserAccount disables a user account by username (wrapper method for Service interface)
func (s *LDAPService) DisableUserAccount(username string) error {
	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Verify that the user is not an admin (mandatory security check)
	userGroups, err := s.GetUserGroups(userDN)
	if err != nil {
		return fmt.Errorf("failed to get user groups for %s: %v", username, err)
	}

	isAdminUser, err := isAdmin(userGroups)
	if err != nil {
		return fmt.Errorf("failed to check if user %s is admin: %v", username, err)
	}
	if isAdminUser {
		return fmt.Errorf("cannot disable admin user %s", username)
	}

	// Set userAccountControl to 546 (NORMAL_ACCOUNT + ACCOUNTDISABLE) to disable the account
	modifyReq := ldapv3.NewModifyRequest(userDN, nil)
	modifyReq.Replace("userAccountControl", []string{"546"})

	err = s.client.Modify(modifyReq)
	if err != nil {
		return fmt.Errorf("failed to disable account for user %s: %v", username, err)
	}

	return nil
}

func (s *LDAPService) SetUserGroups(username string, groups []string) error {
	// TODO: Optimize the get userDN since it also gets in the remove and add user

	// Get user DN
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to find user %s: %v", username, err)
	}

	// Get current groups
	currentGroups, err := s.GetUserGroups(userDN)
	if err != nil {
		return fmt.Errorf("failed to get user groups: %v", err)
	}

	// Convert slices to maps for efficient lookup
	currentGroupsMap := make(map[string]bool)
	for _, group := range currentGroups {
		currentGroupsMap[group] = true
	}

	newGroupsMap := make(map[string]bool)
	for _, group := range groups {
		newGroupsMap[group] = true
	}

	// Find groups to remove (in current but not in new)
	for _, group := range currentGroups {
		if !newGroupsMap[group] {
			if err := s.RemoveUserFromGroup(username, group); err != nil {
				return fmt.Errorf("failed to remove user %s from group %s: %v", username, group, err)
			}
		}
	}

	// Find groups to add (in new but not in current)
	for _, group := range groups {
		if !currentGroupsMap[group] {
			if err := s.AddUserToGroup(username, group); err != nil {
				return fmt.Errorf("failed to add user %s to group %s: %v", username, group, err)
			}
		}
	}

	return nil
}

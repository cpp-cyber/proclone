package ldap

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// =================================================
// Public Functions
// =================================================

func (s *LDAPService) GetUsers() ([]User, error) {
	kaminoUsersGroupDN := "CN=KaminoUsers,OU=KaminoGroups," + s.client.config.BaseDN
	searchRequest := ldapv3.NewSearchRequest(
		s.client.config.BaseDN, ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=*)(memberOf=%s))", kaminoUsersGroupDN), // Filter for users in KaminoUsers group
		[]string{"sAMAccountName", "dn", "whenCreated", "memberOf", "userAccountControl"},       // Attributes to retrieve
		nil,
	)

	searchResult, err := s.client.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search for users: %v", err)
	}

	var users = []User{}
	for _, entry := range searchResult.Entries {
		user := User{
			Name: entry.GetAttributeValue("sAMAccountName"),
		}

		whenCreated := entry.GetAttributeValue("whenCreated")
		if whenCreated != "" {
			// AD stores dates in GeneralizedTime format: YYYYMMDDHHMMSS.0Z
			if parsedTime, err := time.Parse("20060102150405.0Z", whenCreated); err == nil {
				user.CreatedAt = parsedTime.Format("2006-01-02 15:04:05")
			}
		}

		// Check if user is enabled
		userAccountControl := entry.GetAttributeValue("userAccountControl")
		if userAccountControl != "" {
			uac, err := strconv.Atoi(userAccountControl)
			if err == nil {
				// UF_ACCOUNTDISABLE = 0x02
				user.Enabled = (uac & 0x02) == 0
			}
		}

		// Check if user is admin or creator
		memberOfValues := entry.GetAttributeValues("memberOf")
		for _, memberOf := range memberOfValues {
			if strings.Contains(memberOf, s.client.config.AdminGroupName) {
				user.IsAdmin = true
			}
			if strings.Contains(memberOf, s.client.config.CreatorGroupName) {
				user.IsCreator = true
			}
		}

		// Get user groups
		groups, err := getUserGroupsFromMemberOf(memberOfValues)
		if err == nil {
			user.Groups = groups
		}

		users = append(users, user)
	}

	return users, nil
}

func (s *LDAPService) GetUser(username string) (*User, error) {
	kaminoUsersGroupDN := "CN=KaminoUsers,OU=KaminoGroups," + s.client.config.BaseDN
	searchRequest := ldapv3.NewSearchRequest(
		s.client.config.BaseDN, ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s)(memberOf=%s))", username, kaminoUsersGroupDN), // Filter for specific user in KaminoUsers group
		[]string{"sAMAccountName", "dn", "whenCreated", "memberOf", "userAccountControl"},                  // Attributes to retrieve
		nil,
	)

	searchResult, err := s.client.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search for user: %v", err)
	}

	if len(searchResult.Entries) == 0 {
		return nil, fmt.Errorf("user '%s' not found", username)
	}

	entry := searchResult.Entries[0]
	user := User{
		Name: entry.GetAttributeValue("sAMAccountName"),
	}

	whenCreated := entry.GetAttributeValue("whenCreated")
	if whenCreated != "" {
		// AD stores dates in GeneralizedTime format: YYYYMMDDHHMMSS.0Z
		if parsedTime, err := time.Parse("20060102150405.0Z", whenCreated); err == nil {
			user.CreatedAt = parsedTime.Format("2006-01-02 15:04:05")
		}
	}

	// Check if user is enabled
	userAccountControl := entry.GetAttributeValue("userAccountControl")
	if userAccountControl != "" {
		uac, err := strconv.Atoi(userAccountControl)
		if err == nil {
			// UF_ACCOUNTDISABLE = 0x02
			user.Enabled = (uac & 0x02) == 0
		}
	}

	// Check if user is admin or creator
	memberOfValues := entry.GetAttributeValues("memberOf")
	for _, memberOf := range memberOfValues {
		if strings.Contains(memberOf, s.client.config.AdminGroupName) {
			user.IsAdmin = true
		}
		if strings.Contains(memberOf, s.client.config.CreatorGroupName) {
			user.IsCreator = true
		}
	}

	// Get user groups
	groups, err := getUserGroupsFromMemberOf(memberOfValues)
	if err == nil {
		user.Groups = groups
	}

	return &user, nil
}

func (s *LDAPService) CreateUser(userInfo UserRegistrationInfo) (string, error) {
	// Create DN for new user in Users container
	// TODO: Static
	userDN := fmt.Sprintf("CN=%s,OU=KaminoUsers,%s", userInfo.Username, s.client.config.BaseDN)

	// Create add request for new user
	addReq := ldapv3.NewAddRequest(userDN, nil)

	// Add required object classes
	addReq.Attribute("objectClass", []string{"top", "person", "organizationalPerson", "user"})

	// Add basic attributes
	addReq.Attribute("cn", []string{userInfo.Username})
	addReq.Attribute("sAMAccountName", []string{userInfo.Username})
	addReq.Attribute("userPrincipalName", []string{fmt.Sprintf("%s@%s", userInfo.Username, extractDomainFromDN(s.client.config.BaseDN))})

	// Set account control flags - account disabled initially (will be enabled after password is set)
	addReq.Attribute("userAccountControl", []string{"546"}) // NORMAL_ACCOUNT + ACCOUNTDISABLE

	// Perform the add operation
	err := s.client.Add(addReq)
	if err != nil {
		return "", fmt.Errorf("failed to create user: %v", err)
	}

	return userDN, nil
}

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

func (s *LDAPService) EnableUserAccountByDN(userDN string) error {
	modifyRequest := ldapv3.NewModifyRequest(userDN, nil)
	modifyRequest.Replace("userAccountControl", []string{"512"}) // Normal account

	err := s.client.Modify(modifyRequest)
	if err != nil {
		return fmt.Errorf("failed to enable user account: %v", err)
	}

	return nil
}

// DisableUserAccountByDN disables a user account by DN
func (s *LDAPService) DisableUserAccountByDN(userDN string) error {
	modifyRequest := ldapv3.NewModifyRequest(userDN, nil)
	modifyRequest.Replace("userAccountControl", []string{"514"}) // Disabled account

	err := s.client.Modify(modifyRequest)
	if err != nil {
		return fmt.Errorf("failed to disable user account: %v", err)
	}

	return nil
}

func (s *LDAPService) AddToGroup(userDN string, groupDN string) error {
	modifyRequest := ldapv3.NewModifyRequest(groupDN, nil)
	modifyRequest.Add("member", []string{userDN})

	err := s.client.Modify(modifyRequest)
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

func (s *LDAPService) RemoveFromGroup(userDN string, groupDN string) error {
	modifyRequest := ldapv3.NewModifyRequest(groupDN, nil)
	modifyRequest.Delete("member", []string{userDN})

	err := s.client.Modify(modifyRequest)
	if err != nil {
		// Check if the error is because the user is not in the group
		if strings.Contains(strings.ToLower(err.Error()), "no such attribute") ||
			strings.Contains(strings.ToLower(err.Error()), "unwilling to perform") ||
			strings.Contains(strings.ToLower(err.Error()), "no such object") {
			return nil // Not an error if user is not in group
		}
		return fmt.Errorf("failed to remove user from group: %v", err)
	}

	return nil
}

func (s *LDAPService) CreateAndRegisterUser(userInfo UserRegistrationInfo) error {
	// Validate username
	if !isValidUsername(userInfo.Username) {
		return fmt.Errorf("invalid username: must be alphanumeric and 1-20 characters long")
	}

	// Validate password strength
	if len(userInfo.Password) < 8 || len(userInfo.Password) > 128 {
		return fmt.Errorf("password must be between 8 and 128 characters long")
	}

	userDN, err := s.CreateUser(userInfo)
	if err != nil {
		return fmt.Errorf("failed to create user: %v", err)
	}

	// Set password
	err = s.SetUserPassword(userDN, userInfo.Password)
	if err != nil {
		// Clean up created user if password setting fails
		delRequest := ldapv3.NewDelRequest(userDN, nil)
		s.client.Del(delRequest)
		return fmt.Errorf("failed to set user password: %v", err)
	}

	// Enable account
	err = s.EnableUserAccountByDN(userDN)
	if err != nil {
		return fmt.Errorf("failed to enable user account: %v", err)
	}

	// Add user to KaminoUsers group
	kaminoUsersGroupDN := "CN=KaminoUsers,OU=KaminoGroups," + s.client.config.BaseDN
	err = s.AddToGroup(userDN, kaminoUsersGroupDN)
	if err != nil {
		return fmt.Errorf("failed to add user to KaminoUsers group: %v", err)
	}

	return nil
}

func (s *LDAPService) AddUserToGroup(username string, groupName string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("failed to get group DN: %v", err)
	}

	return s.AddToGroup(userDN, groupDN)
}

func (s *LDAPService) RemoveUserFromGroup(username string, groupName string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("failed to get group DN: %v", err)
	}

	modifyRequest := ldapv3.NewModifyRequest(groupDN, nil)
	modifyRequest.Delete("member", []string{userDN})

	err = s.client.Modify(modifyRequest)
	if err != nil {
		return fmt.Errorf("failed to remove user from group: %v", err)
	}

	return nil
}

func (s *LDAPService) DeleteUser(username string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	delRequest := ldapv3.NewDelRequest(userDN, nil)
	err = s.client.Del(delRequest)
	if err != nil {
		return fmt.Errorf("failed to delete user: %v", err)
	}

	return nil
}

func (s *LDAPService) DeleteUsers(usernames []string) []error {
	var errors []error
	for _, username := range usernames {
		err := s.DeleteUser(username)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to delete user %s: %v", username, err))
		}
	}
	return errors
}

func (s *LDAPService) GetUserGroups(userDN string) ([]string, error) {
	searchRequest := ldapv3.NewSearchRequest(
		userDN,
		ldapv3.ScopeBaseObject,
		ldapv3.NeverDerefAliases,
		1,
		30,
		false,
		"(objectClass=*)",
		[]string{"memberOf"},
		nil,
	)

	searchResult, err := s.client.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to search for user groups: %v", err)
	}

	if len(searchResult.Entries) == 0 {
		return []string{}, nil
	}

	memberOfValues := searchResult.Entries[0].GetAttributeValues("memberOf")
	var groups []string
	for _, memberOf := range memberOfValues {
		// Extract CN from DN
		parts := strings.Split(memberOf, ",")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "CN=") {
			groupName := strings.TrimPrefix(parts[0], "CN=")
			groups = append(groups, groupName)
		}
	}

	return groups, nil
}

func (s *LDAPService) EnableUserAccount(username string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	return s.EnableUserAccountByDN(userDN)
}

func (s *LDAPService) DisableUserAccount(username string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	return s.DisableUserAccountByDN(userDN)
}

func (s *LDAPService) SetUserGroups(username string, groups []string) error {
	userDN, err := s.GetUserDN(username)
	if err != nil {
		return fmt.Errorf("failed to get user DN: %v", err)
	}

	// Get current groups
	currentGroups, err := s.GetUserGroups(userDN)
	if err != nil {
		return fmt.Errorf("failed to get current user groups: %v", err)
	}

	// Remove from current groups
	for _, group := range currentGroups {
		err = s.RemoveUserFromGroup(username, group)
		if err != nil {
			return fmt.Errorf("failed to remove user from group %s: %v", group, err)
		}
	}

	// Add to new groups
	for _, group := range groups {
		err = s.AddUserToGroup(username, group)
		if err != nil {
			return fmt.Errorf("failed to add user to group %s: %v", group, err)
		}
	}

	return nil
}

// =================================================
// Private Functions
// =================================================

func getUserGroupsFromMemberOf(memberOfValues []string) ([]Group, error) {
	var groups []Group
	for _, memberOf := range memberOfValues {
		// Extract CN from DN
		parts := strings.Split(memberOf, ",")
		if len(parts) > 0 && strings.HasPrefix(parts[0], "CN=") {
			groupName := strings.TrimPrefix(parts[0], "CN=")
			groups = append(groups, Group{Name: groupName})
		}
	}
	return groups, nil
}

func isValidUsername(username string) bool {
	if len(username) < 1 || len(username) > 20 {
		return false
	}
	matched, _ := regexp.MatchString("^[a-zA-Z0-9]+$", username)
	return matched
}

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

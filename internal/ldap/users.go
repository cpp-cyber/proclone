package ldap

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// =================================================
// Public Functions
// =================================================

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

// =================================================
// Private Functions
// =================================================

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

package auth

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAPConnection holds the LDAP connection and configuration
type LDAPConnection struct {
	conn         *ldap.Conn
	server       string
	baseDN       string
	bindDN       string
	bindPassword string
}

// ConnectToLDAP creates a new LDAP connection and returns it
func ConnectToLDAP() (*LDAPConnection, error) {
	// LDAP configuration from environment variables
	ldapServer := os.Getenv("LDAP_SERVER")
	baseDN := os.Getenv("LDAP_BASE_DN")
	bindDN := os.Getenv("LDAP_BIND_DN")
	bindPassword := os.Getenv("LDAP_BIND_PASSWORD")

	// check LDAP configuration
	if ldapServer == "" || baseDN == "" || bindDN == "" || bindPassword == "" {
		return nil, fmt.Errorf("LDAP configuration is missing")
	}

	// connect to LDAP server
	conn, err := ldap.DialURL("ldap://" + ldapServer + ":389")
	if err != nil {
		return nil, fmt.Errorf("LDAP connection failed: %v", err)
	}

	// bind as service account
	err = conn.Bind(bindDN, bindPassword)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("LDAP service account bind failed: %v", err)
	}

	return &LDAPConnection{
		conn:         conn,
		server:       ldapServer,
		baseDN:       baseDN,
		bindDN:       bindDN,
		bindPassword: bindPassword,
	}, nil
}

// Close closes the LDAP connection
func (lc *LDAPConnection) Close() {
	if lc.conn != nil {
		lc.conn.Close()
	}
}

// AuthenticateUser authenticates a user against LDAP and returns user info
func (lc *LDAPConnection) AuthenticateUser(username, password string) (string, []string, error) {
	// Define search request
	searchRequest := ldap.NewSearchRequest(
		lc.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(sAMAccountName=%s)", username),
		[]string{"dn", "memberOf"},
		nil,
	)

	// search for user
	sr, err := lc.conn.Search(searchRequest)
	if err != nil {
		return "", nil, fmt.Errorf("user not found in LDAP: %v", err)
	}

	// handle user not found
	if len(sr.Entries) != 1 {
		return "", nil, fmt.Errorf("user not found or multiple users found")
	}

	userDN := sr.Entries[0].DN
	groups := sr.Entries[0].GetAttributeValues("memberOf")

	// bind as user to verify password
	err = lc.conn.Bind(userDN, password)
	if err != nil {
		return "", nil, fmt.Errorf("invalid credentials")
	}

	// rebind as service account for further operations
	err = lc.conn.Bind(lc.bindDN, lc.bindPassword)
	if err != nil {
		return "", nil, fmt.Errorf("failed to rebind as service account")
	}

	return userDN, groups, nil
}

// GetAllUsers fetches all users from Active Directory
func (lc *LDAPConnection) GetAllUsers() (*UserResponse, error) {
	// search for all users
	searchRequest := ldap.NewSearchRequest(
		lc.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(&(objectClass=user)(objectCategory=person)(!(userAccountControl:1.2.840.113556.1.4.803:=2)))",
		[]string{"sAMAccountName", "whenCreated", "memberOf"},
		nil,
	)

	// perform search
	sr, err := lc.conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %v", err)
	}

	var userResponse UserResponse
	userResponse.Users = make([]UserWithRoles, 0)

	// process each user entry
	for _, entry := range sr.Entries {
		username := entry.GetAttributeValue("sAMAccountName")
		whenCreated := entry.GetAttributeValue("whenCreated")
		groups := entry.GetAttributeValues("memberOf")

		// skip if no username
		if username == "" {
			continue
		}

		// parse and format creation date
		createdDate := "Unknown"
		if whenCreated != "" {
			// AD stores dates in GeneralizedTime format: YYYYMMDDHHMMSS.0Z
			if parsedTime, err := time.Parse("20060102150405.0Z", whenCreated); err == nil {
				createdDate = parsedTime.Format("2006-01-02 15:04:05")
			}
		}

		// check if user is admin
		isAdmin := false
		for _, group := range groups {
			if strings.Contains(strings.ToLower(group), "cn=domain admins") || strings.Contains(strings.ToLower(group), "cn=kamino admin") {
				isAdmin = true
				break
			}
		}

		// clean up group names (extract CN values)
		cleanGroups := make([]string, 0)
		for _, group := range groups {
			// extract CN from DN format
			parts := strings.Split(group, ",")
			if len(parts) > 0 && strings.HasPrefix(strings.ToLower(parts[0]), "cn=") {
				groupName := strings.TrimPrefix(parts[0], "CN=")
				groupName = strings.TrimPrefix(groupName, "cn=")
				cleanGroups = append(cleanGroups, groupName)
			}
		}

		user := UserWithRoles{
			Username:    username,
			CreatedDate: createdDate,
			IsAdmin:     isAdmin,
			Groups:      cleanGroups,
		}

		userResponse.Users = append(userResponse.Users, user)
	}

	return &userResponse, nil
}

// CheckIfAdmin checks if a user is in the Domain Admins group
func CheckIfAdmin(groups []string) bool {
	for _, group := range groups {
		if strings.Contains(strings.ToLower(group), "cn=domain admins") {
			return true
		}
	}
	return false
}

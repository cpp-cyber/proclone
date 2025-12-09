package ldap

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

// =================================================
// Public Functions
// =================================================

func (s *LDAPService) GetGroups() ([]Group, error) {
	// Search for all groups in the KaminoGroups OU
	kaminoGroupsOU := "OU=KaminoGroups," + s.client.config.BaseDN
	req := ldapv3.NewSearchRequest(
		kaminoGroupsOU,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		"(objectClass=group)",
		[]string{"cn", "whenCreated", "member"},
		nil,
	)

	searchResult, err := s.client.Search(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for groups: %v", err)
	}

	var groups []Group
	for _, entry := range searchResult.Entries {
		cn := entry.GetAttributeValue("cn")

		// Check if the group is protected
		protectedGroup, err := isProtectedGroup(cn)
		if err != nil {
			return nil, fmt.Errorf("failed to determine if the group %s is protected: %v", cn, err)
		}

		group := Group{
			Name:      cn,
			CanModify: !protectedGroup,
			UserCount: len(entry.GetAttributeValues("member")),
		}

		// Add creation date if available and convert it
		whenCreated := entry.GetAttributeValue("whenCreated")
		if whenCreated != "" {
			// AD stores dates in GeneralizedTime format: YYYYMMDDHHMMSS.0Z
			if parsedTime, err := time.Parse("20060102150405.0Z", whenCreated); err == nil {
				group.CreatedAt = parsedTime.Format("2006-01-02 15:04:05")
			}
		}

		groups = append(groups, group)
	}

	return groups, nil
}

func (s *LDAPService) CreateGroup(groupName string) error {
	// Validate group name
	if err := validateGroupName(groupName); err != nil {
		return fmt.Errorf("invalid group name: %v", err)
	}

	// Check if group already exists
	_, err := s.getGroupDN(groupName)
	if err == nil {
		return fmt.Errorf("group already exists: %s", groupName)
	}

	// Construct the DN for the new group
	groupDN := fmt.Sprintf("CN=%s,OU=KaminoGroups,%s", groupName, s.client.config.BaseDN)

	// Create the add request
	addReq := ldapv3.NewAddRequest(groupDN, nil)
	addReq.Attribute("objectClass", []string{"top", "group"})
	addReq.Attribute("cn", []string{groupName})
	addReq.Attribute("sAMAccountName", []string{groupName})
	addReq.Attribute("groupType", []string{"-2147483646"})

	// Execute the add request
	err = s.client.Add(addReq)
	if err != nil {
		return fmt.Errorf("failed to create group: %v", err)
	}

	return nil
}

func (s *LDAPService) RenameGroup(oldGroupName string, newGroupName string) error {
	// Validate new group name
	if err := validateGroupName(newGroupName); err != nil {
		return fmt.Errorf("invalid new group name: %v", err)
	}

	// Check if old group exists
	oldGroupDN, err := s.getGroupDN(oldGroupName)
	if err != nil {
		return fmt.Errorf("old group not found: %v", err)
	}

	// Check if new group already exists
	_, err = s.getGroupDN(newGroupName)
	if err == nil {
		return fmt.Errorf("new group name already exists: %s", newGroupName)
	}

	// Create modify DN request
	newRDN := fmt.Sprintf("CN=%s", newGroupName)
	modifyDNReq := ldapv3.NewModifyDNRequest(oldGroupDN, newRDN, true, "")

	// Execute the modify DN request
	err = s.client.ModifyDN(modifyDNReq)
	if err != nil {
		return fmt.Errorf("failed to rename group: %v", err)
	}

	return nil
}

func (s *LDAPService) DeleteGroup(groupName string) error {
	// Check if group is protected
	protected, err := isProtectedGroup(groupName)
	if err != nil {
		return fmt.Errorf("failed to check if group is protected: %v", err)
	}
	if protected {
		return fmt.Errorf("cannot delete protected group: %s", groupName)
	}

	// Get group DN
	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("group not found: %v", err)
	}

	// Create delete request
	delReq := ldapv3.NewDelRequest(groupDN, nil)

	// Execute the delete request
	err = s.client.Del(delReq)
	if err != nil {
		return fmt.Errorf("failed to delete group: %v", err)
	}

	return nil
}

func (s *LDAPService) GetGroupMembers(groupName string) ([]User, error) {
	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return nil, fmt.Errorf("group not found: %v", err)
	}

	// Search for the group and get its members
	req := ldapv3.NewSearchRequest(
		groupDN,
		ldapv3.ScopeBaseObject, ldapv3.NeverDerefAliases, 0, 0, false,
		"(objectClass=group)",
		[]string{"member"},
		nil,
	)

	searchResult, err := s.client.Search(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for group: %v", err)
	}

	if len(searchResult.Entries) == 0 {
		return []User{}, nil
	}

	memberDNs := searchResult.Entries[0].GetAttributeValues("member")
	var users []User

	for _, memberDN := range memberDNs {
		// Get user details from DN
		userReq := ldapv3.NewSearchRequest(
			memberDN,
			ldapv3.ScopeBaseObject, ldapv3.NeverDerefAliases, 0, 0, false,
			"(objectClass=user)",
			[]string{"sAMAccountName", "cn", "whenCreated", "userAccountControl"},
			nil,
		)

		userResult, err := s.client.Search(userReq)
		if err != nil {
			continue // Skip this user if there's an error
		}

		if len(userResult.Entries) > 0 {
			entry := userResult.Entries[0]
			user := User{
				Name:      entry.GetAttributeValue("sAMAccountName"),
				CreatedAt: entry.GetAttributeValue("whenCreated"),
				Enabled:   true, // Default, will be updated based on userAccountControl
			}

			// Check if user is enabled
			userAccountControl := entry.GetAttributeValue("userAccountControl")
			if userAccountControl != "" {
				// Parse userAccountControl to determine if account is enabled
				// UF_ACCOUNTDISABLE = 0x02
				if strings.Contains(userAccountControl, "2") {
					user.Enabled = false
				}
			}

			users = append(users, user)
		}
	}

	return users, nil
}

func (s *LDAPService) AddUsersToGroup(groupName string, usernames []string) error {
	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("group not found: %v", err)
	}

	// Add users one by one to handle cases where some users might already be in the group
	for _, username := range usernames {
		userDN, err := s.GetUserDN(username)
		if err != nil {
			return fmt.Errorf("user %s not found: %v", username, err)
		}

		if err := s.AddToGroup(userDN, groupDN); err != nil {
			return fmt.Errorf("failed to add user %s to group: %v", username, err)
		}
	}

	return nil
}

func (s *LDAPService) RemoveUsersFromGroup(groupName string, usernames []string) error {
	groupDN, err := s.getGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("group not found: %v", err)
	}

	// Remove users one by one to handle cases where some users might not be in the group
	for _, username := range usernames {
		userDN, err := s.GetUserDN(username)
		if err != nil {
			return fmt.Errorf("user %s not found: %v", username, err)
		}

		if err := s.RemoveFromGroup(userDN, groupDN); err != nil {
			return fmt.Errorf("failed to remove user %s from group: %v", username, err)
		}
	}

	return nil
}

// =================================================
// Private Functions
// =================================================

func (s *LDAPService) getGroupDN(groupName string) (string, error) {
	kaminoGroupsOU := "OU=KaminoGroups," + s.client.config.BaseDN
	req := ldapv3.NewSearchRequest(
		kaminoGroupsOU,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 1, 30, false,
		fmt.Sprintf("(&(objectClass=group)(cn=%s))", ldapv3.EscapeFilter(groupName)),
		[]string{"dn"},
		nil,
	)

	searchResult, err := s.client.Search(req)
	if err != nil {
		return "", fmt.Errorf("failed to search for group: %v", err)
	}

	if len(searchResult.Entries) == 0 {
		return "", fmt.Errorf("group %s not found", groupName)
	}

	return searchResult.Entries[0].DN, nil
}

func validateGroupName(groupName string) error {
	if groupName == "" {
		return fmt.Errorf("group name cannot be empty")
	}

	if len(groupName) >= 64 {
		return fmt.Errorf("group name must be less than 64 characters")
	}

	regex := regexp.MustCompile("^[a-zA-Z0-9-_]*$")
	if !regex.MatchString(groupName) {
		return fmt.Errorf("group name must only contain letters, numbers, hyphens, and underscores")
	}

	return nil
}

func isProtectedGroup(groupName string) (bool, error) {
	protectedGroups := []string{
		"Domain Admins",
		"Domain Users",
		"Domain Guests",
		"Schema Admins",
		"Enterprise Admins",
		"Administrators",
		"Users",
		"Guests",
		"Proxmox-Admins",
		"KaminoUsers",
	}

	for _, protectedGroup := range protectedGroups {
		if strings.EqualFold(groupName, protectedGroup) {
			return true, nil
		}
	}

	return false, nil
}

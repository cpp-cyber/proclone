package auth

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	ldapv3 "github.com/go-ldap/ldap/v3"
)

type CreateRequest struct {
	Group string `json:"group"`
}

type Group struct {
	Name      string `json:"name"`
	CanModify bool   `json:"can_modify"`
	CreatedAt string `json:"created_at,omitempty"`
	UserCount int    `json:"user_count,omitempty"`
}

// GetGroups retrieves all groups from the KaminoGroups OU
func (s *LDAPService) GetGroups() ([]Group, error) {
	log.Println("[DEBUG] GetGroups: Starting to retrieve all groups from KaminoGroups OU")
	config := s.client.Config()

	// Search for all groups in the KaminoGroups OU
	kaminoGroupsOU := "OU=KaminoGroups," + config.BaseDN
	log.Printf("[DEBUG] GetGroups: Searching in OU: %s", kaminoGroupsOU)
	req := ldapv3.NewSearchRequest(
		kaminoGroupsOU,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		"(objectClass=group)",
		[]string{"cn", "whenCreated", "member"},
		nil,
	)

	searchResult, err := s.client.Search(req)
	if err != nil {
		log.Printf("[ERROR] GetGroups: Failed to search for groups: %v", err)
		return nil, fmt.Errorf("failed to search for groups: %v", err)
	}
	log.Printf("[DEBUG] GetGroups: Found %d groups", len(searchResult.Entries))

	var groups []Group
	for _, entry := range searchResult.Entries {
		cn := entry.GetAttributeValue("cn")
		log.Printf("[DEBUG] GetGroups: Processing group: %s", cn)

		// Check if the group is protected
		protectedGroup, err := isProtectedGroup(cn)
		if err != nil {
			log.Printf("[ERROR] GetGroups: Failed to determine if group %s is protected: %v", cn, err)
			return nil, fmt.Errorf("failed to determine if the group %s is protected: %v", cn, err)
		}
		log.Printf("[DEBUG] GetGroups: Group %s protected status: %v", cn, protectedGroup)

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
				log.Printf("[DEBUG] GetGroups: Group %s created at: %s", cn, group.CreatedAt)
			} else {
				log.Printf("[WARN] GetGroups: Failed to parse creation date for group %s: %s", cn, whenCreated)
			}
		}

		log.Printf("[DEBUG] GetGroups: Group %s has %d members", cn, group.UserCount)
		groups = append(groups, group)
	}

	log.Printf("[INFO] GetGroups: Successfully retrieved %d groups", len(groups))
	return groups, nil
}

func ValidateGroupName(groupName string) error {
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

// CreateGroup creates a new group in LDAP
func (s *LDAPService) CreateGroup(groupName string) error {
	log.Printf("[DEBUG] CreateGroup: Starting to create group: %s", groupName)
	config := s.client.Config()

	// Validate group name
	if err := ValidateGroupName(groupName); err != nil {
		log.Printf("[ERROR] CreateGroup: Invalid group name %s: %v", groupName, err)
		return fmt.Errorf("invalid group name: %v", err)
	}
	log.Printf("[DEBUG] CreateGroup: Group name validation passed for: %s", groupName)

	// Check if group already exists
	_, err := s.GetGroupDN(groupName)
	if err == nil {
		log.Printf("[ERROR] CreateGroup: Group already exists: %s", groupName)
		return fmt.Errorf("group already exists: %s", groupName)
	}
	log.Printf("[DEBUG] CreateGroup: Confirmed group %s does not exist", groupName)

	// Construct the DN for the new group
	groupDN := fmt.Sprintf("CN=%s,OU=KaminoGroups,%s", groupName, config.BaseDN)
	log.Printf("[DEBUG] CreateGroup: Creating group with DN: %s", groupDN)

	// Create the add request
	addReq := ldapv3.NewAddRequest(groupDN, nil)
	addReq.Attribute("objectClass", []string{"top", "group"})
	addReq.Attribute("cn", []string{groupName})
	addReq.Attribute("name", []string{groupName})
	addReq.Attribute("sAMAccountName", []string{groupName})
	// Set group type to Global Security Group (0x80000002)
	addReq.Attribute("groupType", []string{"-2147483646"})

	err = s.client.Add(addReq)
	if err != nil {
		log.Printf("[ERROR] CreateGroup: Failed to create group %s: %v", groupName, err)
		return fmt.Errorf("failed to create group %s: %v", groupName, err)
	}

	log.Printf("[INFO] CreateGroup: Successfully created group: %s", groupName)
	return nil
}

// RenameGroup updates an existing group in LDAP
func (s *LDAPService) RenameGroup(oldGroupName string, newGroupName string) error {
	// Check if the group is protected
	protectedGroup, err := isProtectedGroup(oldGroupName)
	if err != nil {
		return fmt.Errorf("failed to determine if the group %s is protected: %v", oldGroupName, err)
	}

	if protectedGroup {
		return fmt.Errorf("cannot rename protected group: %s", oldGroupName)
	}

	// If names are the same, nothing to do
	if oldGroupName == newGroupName {
		return nil
	}

	// Validate new group name
	if err := ValidateGroupName(newGroupName); err != nil {
		return fmt.Errorf("invalid group name: %v", err)
	}

	// Get the DN of the existing group
	groupDN, err := s.GetGroupDN(oldGroupName)
	if err != nil {
		return fmt.Errorf("failed to find group %s: %v", oldGroupName, err)
	}

	// Check if the new name already exists
	_, err = s.GetGroupDN(newGroupName)
	if err == nil {
		return fmt.Errorf("group with name %s already exists", newGroupName)
	}

	// Get config to construct the new DN
	config := s.client.Config()

	// Extract the parent DN (everything after the first comma in the current DN)
	// Example: "CN=OldName,OU=KaminoGroups,DC=example,DC=com" -> "OU=KaminoGroups,DC=example,DC=com"
	parentDN := "OU=KaminoGroups," + config.BaseDN

	// Create new RDN (Relative Distinguished Name)
	newRDN := fmt.Sprintf("CN=%s", newGroupName)

	// Use ModifyDN operation to rename the group
	modifyDNReq := ldapv3.NewModifyDNRequest(groupDN, newRDN, true, parentDN)

	err = s.client.ModifyDN(modifyDNReq)
	if err != nil {
		return fmt.Errorf("failed to rename group %s to %s: %v", oldGroupName, newGroupName, err)
	}

	return nil
}

// DeleteGroup deletes a group from LDAP
func (s *LDAPService) DeleteGroup(groupName string) error {
	log.Printf("[DEBUG] DeleteGroup: Starting to delete group: %s", groupName)

	// Check if the group is protected
	protectedGroup, err := isProtectedGroup(groupName)
	if err != nil {
		log.Printf("[ERROR] DeleteGroup: Failed to determine if group %s is protected: %v", groupName, err)
		return fmt.Errorf("failed to determine if the group %s is protected: %v", groupName, err)
	}

	if protectedGroup {
		log.Printf("[ERROR] DeleteGroup: Cannot delete protected group: %s", groupName)
		return fmt.Errorf("cannot delete protected group: %s", groupName)
	}
	log.Printf("[DEBUG] DeleteGroup: Group %s is not protected, proceeding with deletion", groupName)

	// Get the DN of the group to delete
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		log.Printf("[ERROR] DeleteGroup: Failed to find group %s: %v", groupName, err)
		return fmt.Errorf("failed to find group %s: %v", groupName, err)
	}
	log.Printf("[DEBUG] DeleteGroup: Found group DN: %s", groupDN)

	// Create delete request
	delReq := ldapv3.NewDelRequest(groupDN, nil)

	err = s.client.Delete(delReq)
	if err != nil {
		log.Printf("[ERROR] DeleteGroup: Failed to delete group %s: %v", groupName, err)
		return fmt.Errorf("failed to delete group %s: %v", groupName, err)
	}

	log.Printf("[INFO] DeleteGroup: Successfully deleted group: %s", groupName)
	return nil
}

// GetGroupMembers retrieves all members of a specific group
func (s *LDAPService) GetGroupMembers(groupName string) ([]User, error) {
	config := s.client.Config()

	// Get the group DN
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		return nil, fmt.Errorf("failed to find group %s: %v", groupName, err)
	}

	// Search for users who are members of this group
	req := ldapv3.NewSearchRequest(
		config.BaseDN,
		ldapv3.ScopeWholeSubtree, ldapv3.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=*)(memberOf=%s))", groupDN),
		[]string{"sAMAccountName", "dn", "whenCreated", "userAccountControl"},
		nil,
	)

	searchResult, err := s.client.Search(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search for group members: %v", err)
	}

	var users []User
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

		users = append(users, user)
	}

	return users, nil
}

func isProtectedGroup(groupName string) (bool, error) {
	groupName = strings.ToLower(groupName)

	blocked := []string{"kamino", "domain", "admin"}
	for _, b := range blocked {
		if strings.Contains(groupName, b) {
			return true, nil
		}
	}
	return false, nil
}

func (s *LDAPService) AddUsersToGroup(groupName string, usernames []string) error {
	log.Printf("[DEBUG] AddUsersToGroup: Adding %d users to group %s", len(usernames), groupName)
	if len(usernames) == 0 {
		log.Printf("[DEBUG] AddUsersToGroup: No users to add to group %s", groupName)
		return nil // Nothing to do
	}

	// Get group DN first
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		log.Printf("[ERROR] AddUsersToGroup: Failed to find group %s: %v", groupName, err)
		return fmt.Errorf("failed to find group %s: %v", groupName, err)
	}
	log.Printf("[DEBUG] AddUsersToGroup: Found group DN: %s", groupDN)

	// Get all user DNs, filtering out invalid users
	var validUserDNs []string
	var invalidUsers []string

	log.Printf("[DEBUG] AddUsersToGroup: Validating %d usernames", len(usernames))
	for _, username := range usernames {
		if username == "" {
			log.Printf("[DEBUG] AddUsersToGroup: Skipping empty username")
			continue // Skip empty usernames
		}

		userDN, err := s.GetUserDN(username)
		if err != nil {
			log.Printf("[WARN] AddUsersToGroup: Invalid user %s: %v", username, err)
			invalidUsers = append(invalidUsers, username)
			continue
		}
		log.Printf("[DEBUG] AddUsersToGroup: Valid user %s with DN: %s", username, userDN)
		validUserDNs = append(validUserDNs, userDN)
	}

	// If no valid users found, return error
	if len(validUserDNs) == 0 {
		if len(invalidUsers) > 0 {
			log.Printf("[ERROR] AddUsersToGroup: No valid users found. Invalid users: %s", strings.Join(invalidUsers, ", "))
			return fmt.Errorf("no valid users found. Invalid users: %s", strings.Join(invalidUsers, ", "))
		}
		log.Printf("[ERROR] AddUsersToGroup: No users provided")
		return fmt.Errorf("no users provided")
	}

	log.Printf("[DEBUG] AddUsersToGroup: Attempting bulk add of %d valid users", len(validUserDNs))
	// Add all users to the group in a single LDAP modify operation
	modifyReq := ldapv3.NewModifyRequest(groupDN, nil)
	modifyReq.Add("member", validUserDNs)

	err = s.client.Modify(modifyReq)
	if err != nil {
		log.Printf("[WARN] AddUsersToGroup: Bulk add failed, trying individual adds: %v", err)
		// If bulk add fails, try adding users individually to identify which ones are already members
		var alreadyMembers []string
		var failedUsers []string
		var successCount int

		for i, userDN := range validUserDNs {
			log.Printf("[DEBUG] AddUsersToGroup: Individual add attempt for user %s", usernames[i])
			individualReq := ldapv3.NewModifyRequest(groupDN, nil)
			individualReq.Add("member", []string{userDN})

			individualErr := s.client.Modify(individualReq)
			if individualErr != nil {
				if strings.Contains(strings.ToLower(individualErr.Error()), "already exists") ||
					strings.Contains(strings.ToLower(individualErr.Error()), "attribute or value exists") {
					log.Printf("[DEBUG] AddUsersToGroup: User %s already member", usernames[i])
					alreadyMembers = append(alreadyMembers, usernames[i])
				} else {
					log.Printf("[ERROR] AddUsersToGroup: Failed to add user %s: %v", usernames[i], individualErr)
					failedUsers = append(failedUsers, fmt.Sprintf("%s: %v", usernames[i], individualErr))
				}
			} else {
				log.Printf("[DEBUG] AddUsersToGroup: Successfully added user %s", usernames[i])
				successCount++
			}
		}

		// Prepare result message
		var messages []string
		if successCount > 0 {
			messages = append(messages, fmt.Sprintf("%d users added successfully", successCount))
		}
		if len(alreadyMembers) > 0 {
			messages = append(messages, fmt.Sprintf("%d users already members (skipped): %s", len(alreadyMembers), strings.Join(alreadyMembers, ", ")))
		}
		if len(invalidUsers) > 0 {
			messages = append(messages, fmt.Sprintf("%d invalid users (skipped): %s", len(invalidUsers), strings.Join(invalidUsers, ", ")))
		}
		if len(failedUsers) > 0 {
			messages = append(messages, fmt.Sprintf("%d users failed: %s", len(failedUsers), strings.Join(failedUsers, "; ")))
		}

		if len(failedUsers) > 0 && successCount == 0 {
			log.Printf("[ERROR] AddUsersToGroup: All operations failed: %s", strings.Join(messages, "; "))
			return fmt.Errorf("failed to add users to group %s: %s", groupName, strings.Join(messages, "; "))
		}

		// If some succeeded, just log the status (don't return error for partial success)
		log.Printf("[INFO] AddUsersToGroup result: %s", strings.Join(messages, "; "))
		fmt.Printf("AddUsersToGroup result: %s\n", strings.Join(messages, "; "))
	} else {
		log.Printf("[INFO] AddUsersToGroup: Bulk add successful for group %s", groupName)
	}

	return nil
}

func (s *LDAPService) RemoveUsersFromGroup(groupName string, usernames []string) error {
	if len(usernames) == 0 {
		return nil // Nothing to do
	}

	// Get group DN first
	groupDN, err := s.GetGroupDN(groupName)
	if err != nil {
		return fmt.Errorf("failed to find group %s: %v", groupName, err)
	}

	// Get all user DNs, filtering out invalid users
	var validUserDNs []string
	var invalidUsers []string

	for _, username := range usernames {
		if username == "" {
			continue // Skip empty usernames
		}

		userDN, err := s.GetUserDN(username)
		if err != nil {
			invalidUsers = append(invalidUsers, username)
			continue
		}
		validUserDNs = append(validUserDNs, userDN)
	}

	// If no valid users found, return error
	if len(validUserDNs) == 0 {
		if len(invalidUsers) > 0 {
			return fmt.Errorf("no valid users found. Invalid users: %s", strings.Join(invalidUsers, ", "))
		}
		return fmt.Errorf("no users provided")
	}

	// Remove all users from the group in a single LDAP modify operation
	modifyReq := ldapv3.NewModifyRequest(groupDN, nil)
	modifyReq.Delete("member", validUserDNs)

	err = s.client.Modify(modifyReq)
	if err != nil {
		// If bulk remove fails, try removing users individually to identify which ones are not members
		var notMembers []string
		var failedUsers []string
		var successCount int

		for i, userDN := range validUserDNs {
			individualReq := ldapv3.NewModifyRequest(groupDN, nil)
			individualReq.Delete("member", []string{userDN})

			individualErr := s.client.Modify(individualReq)
			if individualErr != nil {
				if strings.Contains(strings.ToLower(individualErr.Error()), "no such attribute") ||
					strings.Contains(strings.ToLower(individualErr.Error()), "no such value") {
					notMembers = append(notMembers, usernames[i])
				} else {
					failedUsers = append(failedUsers, fmt.Sprintf("%s: %v", usernames[i], individualErr))
				}
			} else {
				successCount++
			}
		}

		// Prepare result message
		var messages []string
		if successCount > 0 {
			messages = append(messages, fmt.Sprintf("%d users removed successfully", successCount))
		}
		if len(notMembers) > 0 {
			messages = append(messages, fmt.Sprintf("%d users not members (skipped): %s", len(notMembers), strings.Join(notMembers, ", ")))
		}
		if len(invalidUsers) > 0 {
			messages = append(messages, fmt.Sprintf("%d invalid users (skipped): %s", len(invalidUsers), strings.Join(invalidUsers, ", ")))
		}
		if len(failedUsers) > 0 {
			messages = append(messages, fmt.Sprintf("%d users failed: %s", len(failedUsers), strings.Join(failedUsers, "; ")))
		}

		if len(failedUsers) > 0 && successCount == 0 {
			return fmt.Errorf("failed to remove users from group %s: %s", groupName, strings.Join(messages, "; "))
		}

		// If some succeeded, just log the status (don't return error for partial success)
		fmt.Printf("RemoveUsersFromGroup result: %s\n", strings.Join(messages, "; "))
	}

	return nil
}

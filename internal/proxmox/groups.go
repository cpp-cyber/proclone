package proxmox

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// =================================================
// Public Functions
// =================================================

func (s *ProxmoxService) GetGroups() ([]Group, error) {
	groups := []Group{}

	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/access/groups",
	}

	var groupsResponse []GroupsResponse
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &groupsResponse); err != nil {
		return nil, fmt.Errorf("failed to get groups: %w", err)
	}

	for _, group := range groupsResponse {
		groups = append(groups, Group{
			Name:      group.Name,
			UserCount: len(strings.Split(group.Users, ",")),
			Comment:   group.Comment,
		})
	}

	return groups, nil
}

func (s *ProxmoxService) CreateGroup(groupName string, comment string) error {
	// Validate group name
	if err := validateGroupName(groupName); err != nil {
		return fmt.Errorf("invalid group name: %v", err)
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "POST",
		Endpoint: "/access/groups",
		RequestBody: map[string]string{
			"groupid": groupName,
			"comment": comment,
		},
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	return nil
}

func (s *ProxmoxService) DeleteGroup(groupName string) error {
	req := tools.ProxmoxAPIRequest{
		Method:   "DELETE",
		Endpoint: fmt.Sprintf("/access/groups/%s", groupName),
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}

	return nil
}

func (s *ProxmoxService) GetGroupMembers(groupName string) ([]string, error) {
	// Search for the group and get its members
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/access/groups/%s", groupName),
	}

	var groupMemebersResponse GroupMembersResponse
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &groupMemebersResponse); err != nil {
		return nil, fmt.Errorf("failed to get groups members of group: %w", err)
	}

	return groupMemebersResponse.Members, nil
}

func (s *ProxmoxService) AddUsersToGroup(groupName string, usernames []string) error {
	// Add users one by one to handle cases where some users might already be in the group
	for _, username := range usernames {
		// Check user's current groups
		userGroups, err := s.getUserGroups(username)
		if err != nil {
			return fmt.Errorf("failed to get user %s groups: %w", username, err)
		}

		// Check if user is already in the group, if not add them
		if !contains(userGroups, groupName) {
			userGroups = append(userGroups, groupName)
		}

		userID := s.getUserID(username)

		req := tools.ProxmoxAPIRequest{
			Method:   "PUT",
			Endpoint: fmt.Sprintf("/access/users/%s", userID),
			RequestBody: map[string]string{
				"groups": strings.Join(userGroups, ","),
			},
		}

		_, err = s.RequestHelper.MakeRequest(req)
		if err != nil {
			return fmt.Errorf("failed to add user %s to group: %v", username, err)
		}
	}

	return nil
}

func (s *ProxmoxService) RemoveUsersFromGroup(groupName string, usernames []string) error {
	// Remove users one by one to handle cases where some users might not be in the group
	for _, username := range usernames {
		// Check user's current groups
		userGroups, err := s.getUserGroups(username)
		if err != nil {
			return fmt.Errorf("failed to get user %s groups: %w", username, err)
		}

		// Check if user is already in the group, if so, remove them
		if contains(userGroups, groupName) {
			userGroups = remove(userGroups, groupName)
		}

		userID := s.getUserID(username)

		req := tools.ProxmoxAPIRequest{
			Method:   "PUT",
			Endpoint: fmt.Sprintf("/access/users/%s", userID),
			RequestBody: map[string]string{
				"groups": strings.Join(userGroups, ","),
			},
		}

		_, err = s.RequestHelper.MakeRequest(req)
		if err != nil {
			return fmt.Errorf("failed to remove user %s from group %s: %v", username, groupName, err)
		}
	}

	return nil
}

func (s *ProxmoxService) EditGroup(groupName string, comment string) error {
	if err := validateGroupName(groupName); err != nil {
		return err
	}

	req := tools.ProxmoxAPIRequest{
		Method:   "PUT",
		Endpoint: fmt.Sprintf("/access/groups/%s", groupName),
		RequestBody: map[string]string{
			"comment": comment,
		},
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to edit group %s: %v", groupName, err)
	}

	return nil
}

// =================================================
// Private Functions
// =================================================

func (s *ProxmoxService) getUserGroups(username string) ([]string, error) {
	userID := s.getUserID(username)

	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/access/users/%s", userID),
	}

	var userResponse ProxmoxUserIDResponse
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &userResponse); err != nil {
		return nil, fmt.Errorf("failed to get user information: %v", err)
	}

	return userResponse.Groups, nil
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

func contains(slice []string, item string) bool {
	if slices.Contains(slice, item) {
		return true
	}
	return false
}

func remove(slice []string, item string) []string {
	result := []string{}
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

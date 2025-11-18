package proxmox

import (
	"fmt"
	"strings"

	"github.com/cpp-cyber/proclone/internal/tools"
)

// =================================================
// Public Functions
// =================================================

// GetUsers retrieves all users from Proxmox
func (s *ProxmoxService) GetUsers() ([]User, error) {
	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: "/access/users?full=1",
	}

	var usersResponse []ProxmoxUserResponse
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &usersResponse); err != nil {
		return nil, fmt.Errorf("failed to get users: %w", err)
	}

	users := []User{}
	for _, userResp := range usersResponse {
		// Extract username without realm suffix
		username := strings.TrimSuffix(userResp.ID, "@"+s.Config.Realm)

		user := User{
			Name:   username,
			Groups: strings.Split(userResp.Groups, ","),
		}

		users = append(users, user)
	}

	return users, nil
}

// GetUser retrieves a specific user from Proxmox
func (s *ProxmoxService) GetUser(username string) (*User, error) {
	userID := s.getUserID(username)

	req := tools.ProxmoxAPIRequest{
		Method:   "GET",
		Endpoint: fmt.Sprintf("/access/users/%s", userID),
	}

	var userResp ProxmoxUserIDResponse
	if err := s.RequestHelper.MakeRequestAndUnmarshal(req, &userResp); err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	user := User{
		Name:   username,
		Groups: userResp.Groups,
	}

	return &user, nil
}

// SetUserGroups sets the groups for a user in Proxmox
func (s *ProxmoxService) SetUserGroups(username string, groups []string) error {
	userID := s.getUserID(username)

	req := tools.ProxmoxAPIRequest{
		Method:   "PUT",
		Endpoint: fmt.Sprintf("/access/users/%s", userID),
		RequestBody: map[string]any{
			"groups": strings.Join(groups, ","),
		},
	}

	_, err := s.RequestHelper.MakeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to set user groups: %w", err)
	}

	return nil
}

// GetUserGroups retrieves the groups a user belongs to
func (s *ProxmoxService) GetUserGroups(username string) ([]string, error) {
	return s.getUserGroups(username)
}

// =================================================
// Private Functions
// =================================================

func (s *ProxmoxService) getUserID(username string) string {
	if strings.Contains(username, "@") {
		return username
	}
	return fmt.Sprintf("%s@%s", username, s.Config.Realm)
}

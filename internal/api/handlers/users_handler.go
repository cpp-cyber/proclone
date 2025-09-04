package handlers

import (
	"fmt"
	"net/http"

	"github.com/cpp-cyber/proclone/internal/auth"
	"github.com/gin-gonic/gin"
)

// ADMIN: GetUsersHandler returns a list of all users
func (h *AuthHandler) GetUsersHandler(c *gin.Context) {
	users, err := h.authService.GetUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve users"})
		return
	}

	var adminCount = 0
	var disabledCount = 0
	for _, user := range users {
		if user.IsAdmin {
			adminCount++
		}
		if !user.Enabled {
			disabledCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"users":          users,
		"count":          len(users),
		"disabled_count": disabledCount,
		"admin_count":    adminCount,
	})
}

// ADMIN: CreateUsersHandler creates new user(s)
func (h *AuthHandler) CreateUsersHandler(c *gin.Context) {
	var newUser AdminCreateUserRequest
	if err := c.ShouldBindJSON(&newUser); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user data"})
		return
	}

	var errors []error
	for _, user := range newUser.Users {
		if err := h.authService.CreateAndRegisterUser(auth.UserRegistrationInfo(user)); err != nil {
			errors = append(errors, fmt.Errorf("failed to create user %s: %v", user.Username, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create users", "details": errors})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Users created successfully"})
}

// ADMIN: DeleteUsersHandler deletes existing user(s)
func (h *AuthHandler) DeleteUsersHandler(c *gin.Context) {
	var req UsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	var errors []error

	for _, username := range req.Usernames {
		if err := h.authService.DeleteUser(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete users", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users deleted successfully"})
}

// ADMIN: EnableUsersHandler enables existing user(s)
func (h *AuthHandler) EnableUsersHandler(c *gin.Context) {
	var req UsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	var errors []error
	for _, username := range req.Usernames {
		if err := h.authService.EnableUserAccount(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to enable user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable users", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users enabled successfully"})
}

// ADMIN: DisableUsersHandler disables existing user(s)
func (h *AuthHandler) DisableUsersHandler(c *gin.Context) {
	var req UsersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	var errors []error

	for _, username := range req.Usernames {
		if err := h.authService.DisableUserAccount(username); err != nil {
			errors = append(errors, fmt.Errorf("failed to disable user %s: %v", username, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to disable users", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users disabled successfully"})
}

// ADMIN: SetUserGroupsHandler sets the groups for an existing user
func (h *AuthHandler) SetUserGroupsHandler(c *gin.Context) {
	var req SetUserGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "details": err.Error()})
		return
	}

	if err := h.authService.SetUserGroups(req.Username, req.Groups); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set user groups", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User groups updated successfully"})
}

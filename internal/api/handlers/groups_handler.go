package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) GetGroupsHandler(c *gin.Context) {
	groups, err := h.authService.GetGroups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve groups"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
		"count":  len(groups),
	})
}

// ADMIN: CreateGroupsHandler creates new group(s)
func (h *AuthHandler) CreateGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group data"})
		return
	}

	var errors []error

	for _, group := range req.Groups {
		if err := h.authService.CreateGroup(group); err != nil {
			errors = append(errors, fmt.Errorf("failed to create group %s: %v", group, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create groups", "details": errors})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Groups created successfully"})
}

func (h *AuthHandler) RenameGroupHandler(c *gin.Context) {
	var req RenameGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group data"})
		return
	}

	if err := h.authService.RenameGroup(req.OldName, req.NewName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Group renamed successfully"})
}

func (h *AuthHandler) DeleteGroupsHandler(c *gin.Context) {
	var req GroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group data"})
		return
	}

	var errors []error

	for _, group := range req.Groups {
		if err := h.authService.DeleteGroup(group); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete group %s: %v", group, err))
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete groups", "details": errors})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Groups deleted successfully"})
}

func (h *AuthHandler) AddUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group data"})
		return
	}

	if err := h.authService.AddUsersToGroup(req.Group, req.Usernames); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add users to group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users added to group successfully"})
}

func (h *AuthHandler) RemoveUsersHandler(c *gin.Context) {
	var req ModifyGroupMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid group data"})
		return
	}

	if err := h.authService.RemoveUsersFromGroup(req.Group, req.Usernames); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove users from group"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Users removed from group successfully"})
}

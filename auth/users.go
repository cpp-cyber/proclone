package auth

import (
	"log"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type UserResponse struct {
	Users []UserWithRoles `json:"users"`
}

type UserWithRoles struct {
	Username    string   `json:"username"`
	CreatedDate string   `json:"createdDate"`
	IsAdmin     bool     `json:"isAdmin"`
	Groups      []string `json:"groups"`
}

// helper function that fetches all users from Active Directory
func buildUserResponse() (*UserResponse, error) {
	// Connect to LDAP
	ldapConn, err := ConnectToLDAP()
	if err != nil {
		return nil, err
	}
	defer ldapConn.Close()

	// Get all users using the LDAP connection
	return ldapConn.GetAllUsers()
}

/*
 * ===== ADMIN ENDPOINT =====
 * This function returns a list of
 * all users and their roles in Active Directory
 */
func GetUsers(c *gin.Context) {
	session := sessions.Default(c)
	username := session.Get("username")
	isAdmin := session.Get("is_admin")

	// make sure user is authenticated and is admin
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt by user %s", username)
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can see all domain users",
		})
		return
	}

	// fetch user response
	userResponse, err := getAdminUserResponse()

	// if error, return error status
	if err != nil {
		log.Printf("Failed to fetch user list for admin %s: %v", username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to fetch user list from Active Directory",
			"details": err.Error(),
		})
		return
	}

	log.Printf("Successfully fetched user list for admin %s", username)
	c.JSON(http.StatusOK, userResponse)
}

func getAdminUserResponse() (*UserResponse, error) {
	return buildUserResponse()
}

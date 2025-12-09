package auth

import (
	"github.com/cpp-cyber/proclone/internal/ldap"
)

// =================================================
// Auth Service Interface
// =================================================

type Service interface {
	// Authentication
	Authenticate(username, password string) (bool, error)
	IsAdmin(username string) (bool, error)
	IsCreator(username string) (bool, error)

	// Health and Connection
	HealthCheck() error
	Reconnect() error
}

type AuthService struct {
	ldapService ldap.Service
}

// =================================================
// Types for Auth Service (re-exported from ldap)
// =================================================

type User = ldap.User
type Group = ldap.Group
type UserRegistrationInfo = ldap.UserRegistrationInfo

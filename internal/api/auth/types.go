package auth

import (
	"github.com/cpp-cyber/proclone/internal/ldap"
	"github.com/cpp-cyber/proclone/internal/proxmox"
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
	ldapService    ldap.Service
	proxmoxService *proxmox.ProxmoxService
}

// =================================================
// Types for Auth Service (re-exported from ldap)
// =================================================

type User = proxmox.User
type Group = proxmox.Group
type UserRegistrationInfo = ldap.UserRegistrationInfo

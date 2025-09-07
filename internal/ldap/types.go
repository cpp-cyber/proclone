package ldap

import (
	"sync"

	"github.com/go-ldap/ldap/v3"
)

// =================================================
// LDAP Service Interface
// =================================================

type Service interface {
	// User Management
	GetUsers() ([]User, error)
	CreateAndRegisterUser(userInfo UserRegistrationInfo) error
	DeleteUser(username string) error
	AddUserToGroup(username string, groupName string) error
	SetUserGroups(username string, groups []string) error
	EnableUserAccount(username string) error
	DisableUserAccount(username string) error
	GetUserGroups(userDN string) ([]string, error)
	GetUserDN(username string) (string, error)

	// Group Management
	CreateGroup(groupName string) error
	GetGroups() ([]Group, error)
	RenameGroup(oldGroupName string, newGroupName string) error
	DeleteGroup(groupName string) error
	GetGroupMembers(groupName string) ([]User, error)
	RemoveUserFromGroup(username string, groupName string) error
	AddUsersToGroup(groupName string, usernames []string) error
	RemoveUsersFromGroup(groupName string, usernames []string) error

	// Connection Management
	HealthCheck() error
	Reconnect() error
	Close() error
}

type LDAPService struct {
	client *Client
}

// =================================================
// LDAP Client
// =================================================

type Config struct {
	URL           string `envconfig:"LDAP_URL" default:"ldaps://localhost:636"`
	BindUser      string `envconfig:"LDAP_BIND_USER"`
	BindPassword  string `envconfig:"LDAP_BIND_PASSWORD"`
	SkipTLSVerify bool   `envconfig:"LDAP_SKIP_TLS_VERIFY" default:"false"`
	AdminGroupDN  string `envconfig:"LDAP_ADMIN_GROUP_DN"`
	BaseDN        string `envconfig:"LDAP_BASE_DN"`
}

type Client struct {
	conn      ldap.Client
	config    *Config
	mutex     sync.RWMutex
	connected bool
}

// =================================================
// Groups
// =================================================

type CreateRequest struct {
	Group string `json:"group"`
}

type Group struct {
	Name      string `json:"name"`
	CanModify bool   `json:"can_modify"`
	CreatedAt string `json:"created_at,omitempty"`
	UserCount int    `json:"user_count,omitempty"`
}

// =================================================
// Users
// =================================================

type User struct {
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	Enabled   bool    `json:"enabled"`
	IsAdmin   bool    `json:"is_admin"`
	Groups    []Group `json:"groups"`
}

type UserRegistrationInfo struct {
	Username string `json:"username" validate:"required,min=1,max=20"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

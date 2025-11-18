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
	CreateAndRegisterUser(userInfo UserRegistrationInfo) error
	DeleteUser(username string) error
	GetUserDN(username string) (string, error)

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

// =================================================
// Users
// =================================================

type UserRegistrationInfo struct {
	Username string `json:"username" validate:"required,min=1,max=20"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

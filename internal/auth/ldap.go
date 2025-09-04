package auth

import (
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/kelseyhightower/envconfig"
)

// Config holds LDAP configuration
type Config struct {
	URL           string `envconfig:"LDAP_URL" default:"ldaps://localhost:636"`
	BindUser      string `envconfig:"LDAP_BIND_USER"`
	BindPassword  string `envconfig:"LDAP_BIND_PASSWORD"`
	SkipTLSVerify bool   `envconfig:"LDAP_SKIP_TLS_VERIFY" default:"false"`
	AdminGroupDN  string `envconfig:"LDAP_ADMIN_GROUP_DN"`
	BaseDN        string `envconfig:"LDAP_BASE_DN"`
}

// Client wraps LDAP connection and provides low-level operations
type Client struct {
	conn      ldap.Client
	config    *Config
	mutex     sync.RWMutex
	connected bool
}

// NewClient creates a new LDAP client
func NewClient(config *Config) *Client {
	return &Client{config: config}
}

// LoadConfig loads and validates LDAP configuration from environment variables
func LoadConfig() (*Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return nil, fmt.Errorf("failed to process LDAP configuration: %w", err)
	}
	return &config, nil
}

// Connectivity

// Connect establishes connection to LDAP server
func (c *Client) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	conn, err := c.dial()
	if err != nil {
		c.connected = false
		return fmt.Errorf("failed to connect to LDAP server: %v", err)
	}

	if c.config.BindUser != "" {
		err = conn.Bind(c.config.BindUser, c.config.BindPassword)
		if err != nil {
			conn.Close()
			c.connected = false
			return fmt.Errorf("failed to bind to LDAP server: %v", err)
		}
	}

	c.conn = conn
	c.connected = true
	return nil
}

// dial creates a new LDAP connection
func (c *Client) dial() (ldap.Client, error) {
	var dialOpts []ldap.DialOpt
	if strings.HasPrefix(c.config.URL, "ldaps://") {
		dialOpts = append(dialOpts, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: c.config.SkipTLSVerify, MinVersion: tls.VersionTLS12}))
	} else {
		return nil, fmt.Errorf("only ldaps:// is supported")
	}
	return ldap.DialURL(c.config.URL, dialOpts...)
}

// Disconnect closes the LDAP connection
func (c *Client) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		c.connected = false
		return nil
	}

	err := c.conn.Close()
	c.connected = false
	return err
}

// isConnectionError checks if an error indicates a connection problem
func (c *Client) isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())
	return strings.Contains(errorMsg, "connection closed") ||
		strings.Contains(errorMsg, "network error") ||
		strings.Contains(errorMsg, "connection reset") ||
		strings.Contains(errorMsg, "broken pipe") ||
		strings.Contains(errorMsg, "connection refused") ||
		strings.Contains(errorMsg, "timeout") ||
		strings.Contains(errorMsg, "eof") ||
		strings.Contains(errorMsg, "operations error") ||
		strings.Contains(errorMsg, "successful bind must be completed") ||
		strings.Contains(errorMsg, "ldap result code 1")
}

// reconnect attempts to reconnect to the LDAP server
func (c *Client) reconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close existing connection if any
	if c.conn != nil {
		c.conn.Close()
	}
	c.connected = false

	// Wait a moment before retrying
	time.Sleep(100 * time.Millisecond)

	// Attempt reconnection
	conn, err := c.dial()
	if err != nil {
		return fmt.Errorf("failed to reconnect to LDAP server: %v", err)
	}

	if c.config.BindUser != "" {
		err = conn.Bind(c.config.BindUser, c.config.BindPassword)
		if err != nil {
			conn.Close()
			return fmt.Errorf("failed to bind after reconnection: %v", err)
		}
	}

	c.conn = conn
	c.connected = true
	return nil
}

// Bind performs LDAP bind operation
func (c *Client) Bind(userDN, password string) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Bind(userDN, password)
	}, 2) // Retry up to 2 times
}

// Config returns the LDAP configuration
func (c *Client) Config() *Config {
	return c.config
}

// IsConnected returns the current connection status
func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

// validateBind checks if the current bind is still valid by performing a simple operation
func (c *Client) validateBind() error {
	c.mutex.RLock()
	conn := c.conn
	c.mutex.RUnlock()

	if conn == nil {
		return fmt.Errorf("no connection available")
	}

	// Try a simple search to validate the bind
	req := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)",
		[]string{"dn"},
		nil,
	)

	_, err := conn.Search(req)
	return err
}

// HealthCheck performs a simple search to verify the connection is working
func (c *Client) HealthCheck() error {
	req := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)",
		[]string{"dn"},
		nil,
	)

	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		_, err := conn.Search(req)
		return err
	}, 2)
}

/*
	Operations
*/

// executeWithRetry executes an LDAP operation with automatic retry on connection errors
func (c *Client) executeWithRetry(operation func() error, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.mutex.RLock()
		connected := c.connected
		conn := c.conn
		c.mutex.RUnlock()

		// Check if we need to reconnect
		if !connected || conn == nil {
			if reconnectErr := c.reconnect(); reconnectErr != nil {
				lastErr = reconnectErr
				continue
			}
		} else {
			// Validate that the bind is still active
			if bindErr := c.validateBind(); bindErr != nil {
				if c.isConnectionError(bindErr) {
					if reconnectErr := c.reconnect(); reconnectErr != nil {
						lastErr = reconnectErr
						continue
					}
				}
			}
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// If it's not a connection error, don't retry
		if !c.isConnectionError(err) {
			return err
		}

		// Mark as disconnected and try to reconnect
		c.mutex.Lock()
		c.connected = false
		c.mutex.Unlock()

		// Don't reconnect on the last attempt
		if attempt < maxRetries {
			if reconnectErr := c.reconnect(); reconnectErr != nil {
				lastErr = reconnectErr
			}
		}
	}

	return fmt.Errorf("operation failed after %d retries, last error: %v", maxRetries+1, lastErr)
}

// SearchEntry performs an LDAP search and returns the first entry
func (c *Client) SearchEntry(req *ldap.SearchRequest) (*ldap.Entry, error) {
	var result *ldap.SearchResult

	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		res, err := conn.Search(req)
		if err != nil {
			return fmt.Errorf("failed to search entry: %v", err)
		}
		result = res
		return nil
	}, 2) // Retry up to 2 times

	if err != nil {
		return nil, err
	}

	if len(result.Entries) == 0 {
		return nil, nil
	}
	return result.Entries[0], nil
}

// Search performs an LDAP search and returns all matching entries
func (c *Client) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	var result *ldap.SearchResult

	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		res, err := conn.Search(req)
		if err != nil {
			return fmt.Errorf("failed to search: %v", err)
		}
		result = res
		return nil
	}, 2) // Retry up to 2 times

	if err != nil {
		return nil, err
	}

	return result, nil
}

// Add performs an LDAP add operation
func (c *Client) Add(req *ldap.AddRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Add(req)
	}, 2) // Retry up to 2 times
}

// Modify performs an LDAP modify operation
func (c *Client) Modify(req *ldap.ModifyRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Modify(req)
	}, 2) // Retry up to 2 times
}

// Delete performs an LDAP delete operation
func (c *Client) Delete(req *ldap.DelRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Del(req)
	}, 2) // Retry up to 2 times
}

// ModifyDN performs an LDAP modify DN operation (rename/move)
func (c *Client) ModifyDN(req *ldap.ModifyDNRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.ModifyDN(req)
	}, 2) // Retry up to 2 times
}

/*
	DNs
*/

// GetUserDN retrieves the DN for a given username
func (s *LDAPService) GetUserDN(username string) (string, error) {
	config := s.client.Config()

	req := ldap.NewSearchRequest(
		config.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", username),
		[]string{"dn"},
		nil,
	)

	entry, err := s.client.SearchEntry(req)
	if err != nil {
		return "", fmt.Errorf("failed to search for user: %v", err)
	}

	if entry == nil {
		return "", fmt.Errorf("user not found")
	}

	return entry.DN, nil
}

// GetGroupDN retrieves the DN for a given group name from KaminoGroups OU
func (s *LDAPService) GetGroupDN(groupName string) (string, error) {
	config := s.client.Config()

	// Search for the group in KaminoGroups OU
	kaminoGroupsOU := "OU=KaminoGroups," + config.BaseDN
	req := ldap.NewSearchRequest(
		kaminoGroupsOU,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=group)(cn=%s))", groupName),
		[]string{"dn"},
		nil,
	)

	entry, err := s.client.SearchEntry(req)
	if err != nil {
		return "", fmt.Errorf("failed to search for group: %v", err)
	}

	if entry == nil {
		return "", fmt.Errorf("group not found: %s", groupName)
	}

	return entry.DN, nil
}

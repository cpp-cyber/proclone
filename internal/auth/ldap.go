package auth

import (
	"crypto/tls"
	"fmt"
	"log"
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
	log.Println("[DEBUG] LoadConfig: Loading LDAP configuration from environment variables")
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		log.Printf("[ERROR] LoadConfig: Failed to process LDAP configuration: %v", err)
		return nil, fmt.Errorf("failed to process LDAP configuration: %w", err)
	}
	log.Printf("[DEBUG] LoadConfig: LDAP configuration loaded - URL: %s, BaseDN: %s, AdminGroupDN: %s",
		config.URL, config.BaseDN, config.AdminGroupDN)
	return &config, nil
}

// Connectivity

// Connect establishes connection to LDAP server
func (c *Client) Connect() error {
	log.Println("[DEBUG] LDAP Connect: Attempting to establish LDAP connection")
	c.mutex.Lock()
	defer c.mutex.Unlock()

	conn, err := c.dial()
	if err != nil {
		log.Printf("[ERROR] LDAP Connect: Failed to dial LDAP server: %v", err)
		c.connected = false
		return fmt.Errorf("failed to connect to LDAP server: %v", err)
	}
	log.Printf("[DEBUG] LDAP Connect: Successfully dialed LDAP server: %s", c.config.URL)

	if c.config.BindUser != "" {
		log.Printf("[DEBUG] LDAP Connect: Binding as service user: %s", c.config.BindUser)
		err = conn.Bind(c.config.BindUser, c.config.BindPassword)
		if err != nil {
			log.Printf("[ERROR] LDAP Connect: Failed to bind as service user: %v", err)
			conn.Close()
			c.connected = false
			return fmt.Errorf("failed to bind to LDAP server: %v", err)
		}
		log.Println("[DEBUG] LDAP Connect: Service user bind successful")
	} else {
		log.Println("[DEBUG] LDAP Connect: No bind user configured, using anonymous bind")
	}

	c.conn = conn
	c.connected = true
	log.Println("[INFO] LDAP Connect: Connection established successfully")
	return nil
}

// dial creates a new LDAP connection
func (c *Client) dial() (ldap.Client, error) {
	log.Printf("[DEBUG] LDAP dial: Attempting to dial %s", c.config.URL)
	var dialOpts []ldap.DialOpt
	if strings.HasPrefix(c.config.URL, "ldaps://") {
		log.Printf("[DEBUG] LDAP dial: Using LDAPS with TLS config - SkipTLSVerify: %v", c.config.SkipTLSVerify)
		dialOpts = append(dialOpts, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: c.config.SkipTLSVerify, MinVersion: tls.VersionTLS12}))
	} else {
		log.Printf("[ERROR] LDAP dial: Unsupported URL scheme: %s", c.config.URL)
		return nil, fmt.Errorf("only ldaps:// is supported")
	}

	conn, err := ldap.DialURL(c.config.URL, dialOpts...)
	if err != nil {
		log.Printf("[ERROR] LDAP dial: Failed to dial %s: %v", c.config.URL, err)
	} else {
		log.Printf("[DEBUG] LDAP dial: Successfully dialed %s", c.config.URL)
	}
	return conn, err
}

// Disconnect closes the LDAP connection
func (c *Client) Disconnect() error {
	log.Println("[DEBUG] LDAP Disconnect: Closing LDAP connection")
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		log.Println("[DEBUG] LDAP Disconnect: No connection to close")
		c.connected = false
		return nil
	}

	err := c.conn.Close()
	c.connected = false
	if err != nil {
		log.Printf("[ERROR] LDAP Disconnect: Error closing connection: %v", err)
	} else {
		log.Println("[INFO] LDAP Disconnect: Connection closed successfully")
	}
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
	log.Println("[DEBUG] LDAP reconnect: Attempting to reconnect to LDAP server")
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close existing connection if any
	if c.conn != nil {
		log.Println("[DEBUG] LDAP reconnect: Closing existing connection")
		c.conn.Close()
	}
	c.connected = false

	// Wait a moment before retrying
	log.Println("[DEBUG] LDAP reconnect: Waiting 100ms before retry")
	time.Sleep(100 * time.Millisecond)

	// Attempt reconnection
	log.Println("[DEBUG] LDAP reconnect: Attempting to dial server")
	conn, err := c.dial()
	if err != nil {
		log.Printf("[ERROR] LDAP reconnect: Failed to reconnect: %v", err)
		return fmt.Errorf("failed to reconnect to LDAP server: %v", err)
	}

	if c.config.BindUser != "" {
		log.Printf("[DEBUG] LDAP reconnect: Rebinding as service user: %s", c.config.BindUser)
		err = conn.Bind(c.config.BindUser, c.config.BindPassword)
		if err != nil {
			log.Printf("[ERROR] LDAP reconnect: Failed to bind after reconnection: %v", err)
			conn.Close()
			return fmt.Errorf("failed to bind after reconnection: %v", err)
		}
		log.Println("[DEBUG] LDAP reconnect: Service user rebind successful")
	}

	c.conn = conn
	c.connected = true
	log.Println("[INFO] LDAP reconnect: Reconnection successful")
	return nil
}

// Bind performs LDAP bind operation
func (c *Client) Bind(userDN, password string) error {
	log.Printf("[DEBUG] LDAP Bind: Attempting to bind as user: %s", userDN)
	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			log.Println("[ERROR] LDAP Bind: No LDAP connection available")
			return fmt.Errorf("no LDAP connection available")
		}

		bindErr := conn.Bind(userDN, password)
		if bindErr != nil {
			log.Printf("[DEBUG] LDAP Bind: Bind failed for %s: %v", userDN, bindErr)
		} else {
			log.Printf("[DEBUG] LDAP Bind: Bind successful for %s", userDN)
		}
		return bindErr
	}, 2) // Retry up to 2 times

	if err != nil {
		log.Printf("[ERROR] LDAP Bind: Final bind failure for %s: %v", userDN, err)
	}
	return err
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
	log.Println("[DEBUG] LDAP HealthCheck: Performing health check")
	req := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)",
		[]string{"dn"},
		nil,
	)

	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			log.Println("[ERROR] LDAP HealthCheck: No LDAP connection available")
			return fmt.Errorf("no LDAP connection available")
		}

		_, searchErr := conn.Search(req)
		if searchErr != nil {
			log.Printf("[DEBUG] LDAP HealthCheck: Search failed: %v", searchErr)
		} else {
			log.Println("[DEBUG] LDAP HealthCheck: Search successful")
		}
		return searchErr
	}, 2)

	if err != nil {
		log.Printf("[ERROR] LDAP HealthCheck: Health check failed: %v", err)
	} else {
		log.Println("[DEBUG] LDAP HealthCheck: Health check passed")
	}
	return err
}

/*
	Operations
*/

// executeWithRetry executes an LDAP operation with automatic retry on connection errors
func (c *Client) executeWithRetry(operation func() error, maxRetries int) error {
	var lastErr error
	log.Printf("[DEBUG] executeWithRetry: Starting operation with max %d retries", maxRetries)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		log.Printf("[DEBUG] executeWithRetry: Attempt %d/%d", attempt+1, maxRetries+1)

		c.mutex.RLock()
		connected := c.connected
		conn := c.conn
		c.mutex.RUnlock()

		// Check if we need to reconnect
		if !connected || conn == nil {
			log.Printf("[DEBUG] executeWithRetry: Connection not available (connected: %v, conn: %v), attempting reconnect", connected, conn != nil)
			if reconnectErr := c.reconnect(); reconnectErr != nil {
				log.Printf("[ERROR] executeWithRetry: Reconnect attempt %d failed: %v", attempt+1, reconnectErr)
				lastErr = reconnectErr
				continue
			}
		} else {
			// Validate that the bind is still active
			log.Println("[DEBUG] executeWithRetry: Validating existing bind")
			if bindErr := c.validateBind(); bindErr != nil {
				if c.isConnectionError(bindErr) {
					log.Printf("[DEBUG] executeWithRetry: Bind validation failed with connection error: %v", bindErr)
					if reconnectErr := c.reconnect(); reconnectErr != nil {
						log.Printf("[ERROR] executeWithRetry: Reconnect after bind validation failure: %v", reconnectErr)
						lastErr = reconnectErr
						continue
					}
				}
			}
		}

		log.Printf("[DEBUG] executeWithRetry: Executing operation (attempt %d)", attempt+1)
		err := operation()
		if err == nil {
			log.Printf("[DEBUG] executeWithRetry: Operation successful on attempt %d", attempt+1)
			return nil
		}

		lastErr = err
		log.Printf("[DEBUG] executeWithRetry: Operation failed on attempt %d: %v", attempt+1, err)

		// If it's not a connection error, don't retry
		if !c.isConnectionError(err) {
			log.Printf("[DEBUG] executeWithRetry: Not a connection error, not retrying: %v", err)
			return err
		}

		// Mark as disconnected and try to reconnect
		log.Println("[DEBUG] executeWithRetry: Connection error detected, marking as disconnected")
		c.mutex.Lock()
		c.connected = false
		c.mutex.Unlock()

		// Don't reconnect on the last attempt
		if attempt < maxRetries {
			log.Printf("[DEBUG] executeWithRetry: Attempting reconnect before retry %d", attempt+2)
			if reconnectErr := c.reconnect(); reconnectErr != nil {
				log.Printf("[ERROR] executeWithRetry: Pre-retry reconnect failed: %v", reconnectErr)
				lastErr = reconnectErr
			}
		}
	}

	log.Printf("[ERROR] executeWithRetry: Operation failed after %d retries, last error: %v", maxRetries+1, lastErr)
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
	log.Printf("[DEBUG] GetUserDN: Searching for user DN for username: %s", username)
	config := s.client.Config()

	req := ldap.NewSearchRequest(
		config.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", username),
		[]string{"dn"},
		nil,
	)
	log.Printf("[DEBUG] GetUserDN: Search filter: (&(objectClass=user)(sAMAccountName=%s)), BaseDN: %s", username, config.BaseDN)

	entry, err := s.client.SearchEntry(req)
	if err != nil {
		log.Printf("[ERROR] GetUserDN: Failed to search for user %s: %v", username, err)
		return "", fmt.Errorf("failed to search for user: %v", err)
	}

	if entry == nil {
		log.Printf("[ERROR] GetUserDN: User not found: %s", username)
		return "", fmt.Errorf("user not found")
	}

	log.Printf("[DEBUG] GetUserDN: Found user DN for %s: %s", username, entry.DN)
	return entry.DN, nil
}

// GetGroupDN retrieves the DN for a given group name from KaminoGroups OU
func (s *LDAPService) GetGroupDN(groupName string) (string, error) {
	log.Printf("[DEBUG] GetGroupDN: Searching for group DN for group: %s", groupName)
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
	log.Printf("[DEBUG] GetGroupDN: Search filter: (&(objectClass=group)(cn=%s)), SearchBase: %s", groupName, kaminoGroupsOU)

	entry, err := s.client.SearchEntry(req)
	if err != nil {
		log.Printf("[ERROR] GetGroupDN: Failed to search for group %s: %v", groupName, err)
		return "", fmt.Errorf("failed to search for group: %v", err)
	}

	if entry == nil {
		log.Printf("[ERROR] GetGroupDN: Group not found: %s", groupName)
		return "", fmt.Errorf("group not found: %s", groupName)
	}

	log.Printf("[DEBUG] GetGroupDN: Found group DN for %s: %s", groupName, entry.DN)
	return entry.DN, nil
}

package ldap

import (
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/kelseyhightower/envconfig"
)

func NewClient(config *Config) *Client {
	return &Client{config: config}
}

func LoadConfig() (*Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return nil, fmt.Errorf("failed to process LDAP configuration: %w", err)
	}
	return &config, nil
}

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
	} else {
	}

	c.conn = conn
	c.connected = true
	return nil
}

func (c *Client) dial() (ldap.Client, error) {
	if strings.HasPrefix(c.config.URL, "ldaps://") {
		return ldap.DialURL(c.config.URL, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: c.config.SkipTLSVerify}))
	} else {
		return nil, fmt.Errorf("unsupported LDAP URL scheme: %s", c.config.URL)
	}
}

func (c *Client) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	return nil
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

func (c *Client) HealthCheck() error {
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
			return fmt.Errorf("no LDAP connection available")
		}

		_, searchErr := conn.Search(req)
		if searchErr != nil {
		} else {
		}
		return searchErr
	}, 2)

	if err != nil {
	} else {
	}
	return err
}

func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

func (c *Client) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	var result *ldap.SearchResult

	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		res, err := conn.Search(searchRequest)
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

func (c *Client) Add(addRequest *ldap.AddRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Add(addRequest)
	}, 2) // Retry up to 2 times
}

func (c *Client) Modify(modifyRequest *ldap.ModifyRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Modify(modifyRequest)
	}, 2) // Retry up to 2 times
}

func (c *Client) Del(delRequest *ldap.DelRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Del(delRequest)
	}, 2) // Retry up to 2 times
}

func (c *Client) ModifyDN(modifyDNRequest *ldap.ModifyDNRequest) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.ModifyDN(modifyDNRequest)
	}, 2) // Retry up to 2 times
}

func (c *Client) Bind(username, password string) error {
	err := c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		bindErr := conn.Bind(username, password)
		if bindErr != nil {
		} else {
		}
		return bindErr
	}, 2) // Retry up to 2 times

	if err != nil {
	}
	return err
}

func (c *Client) SimpleBind(username, password string) error {
	return c.executeWithRetry(func() error {
		c.mutex.RLock()
		conn := c.conn
		c.mutex.RUnlock()

		if conn == nil {
			return fmt.Errorf("no LDAP connection available")
		}

		return conn.Bind(username, password)
	}, 2) // Retry up to 2 times
}

func (s *LDAPService) GetUserDN(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	req := ldap.NewSearchRequest(
		s.client.config.BaseDN,
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

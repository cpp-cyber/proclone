package ldap

import (
	"crypto/tls"
	"fmt"
	"strings"

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

func (c *Client) HealthCheck() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	// Try a simple search to verify the connection
	searchRequest := ldap.NewSearchRequest(
		c.config.BaseDN,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		1,
		1,
		false,
		"(objectClass=*)",
		[]string{"objectClass"},
		nil,
	)

	_, err := c.conn.Search(searchRequest)
	if err != nil {
		return fmt.Errorf("LDAP health check failed: %v", err)
	}

	return nil
}

func (c *Client) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

func (c *Client) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return nil, fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Search(searchRequest)
}

func (c *Client) Add(addRequest *ldap.AddRequest) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Add(addRequest)
}

func (c *Client) Modify(modifyRequest *ldap.ModifyRequest) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Modify(modifyRequest)
}

func (c *Client) Del(delRequest *ldap.DelRequest) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Del(delRequest)
}

func (c *Client) ModifyDN(modifyDNRequest *ldap.ModifyDNRequest) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.ModifyDN(modifyDNRequest)
}

func (c *Client) Bind(username, password string) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Bind(username, password)
}

func (c *Client) SimpleBind(username, password string) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("LDAP connection is not established")
	}

	return c.conn.Bind(username, password)
}

func (s *LDAPService) GetUserDN(username string) (string, error) {
	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	searchRequest := ldap.NewSearchRequest(
		s.client.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1,
		30,
		false,
		fmt.Sprintf("(&(objectClass=inetOrgPerson)(uid=%s))", ldap.EscapeFilter(username)),
		[]string{"dn"},
		nil,
	)

	searchResult, err := s.client.Search(searchRequest)
	if err != nil {
		return "", fmt.Errorf("failed to search for user: %v", err)
	}

	if len(searchResult.Entries) == 0 {
		return "", fmt.Errorf("user %s not found", username)
	}

	return searchResult.Entries[0].DN, nil
}

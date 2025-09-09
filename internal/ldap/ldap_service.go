package ldap

import "fmt"

func NewLDAPService() (*LDAPService, error) {
	config, err := LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load LDAP configuration: %w", err)
	}

	client := NewClient(config)
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}

	return &LDAPService{
		client: client,
	}, nil
}

func (s *LDAPService) Close() error {
	err := s.client.Disconnect()
	if err != nil {
		return err
	}
	return nil
}

func (s *LDAPService) HealthCheck() error {
	err := s.client.HealthCheck()
	if err != nil {
		return err
	}

	return nil
}

func (s *LDAPService) Reconnect() error {
	err := s.client.Connect()
	if err != nil {
		return err
	}
	return nil
}

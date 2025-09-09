package tools

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string `envconfig:"DB_HOST" required:"true"`
	Port     string `envconfig:"DB_PORT" required:"true"`
	User     string `envconfig:"DB_USER" required:"true"`
	Password string `envconfig:"DB_PASSWORD" required:"true"`
	Name     string `envconfig:"DB_NAME" required:"true"`
}

// DBClient wraps database connection and provides reconnection capabilities
type DBClient struct {
	db        *sql.DB
	config    *DatabaseConfig
	mutex     sync.RWMutex
	connected bool
}

// NewDBClient creates a new database client with reconnection capabilities
func NewDBClient() (*DBClient, error) {
	var dbConfig DatabaseConfig
	if err := envconfig.Process("", &dbConfig); err != nil {
		return nil, fmt.Errorf("failed to process database configuration: %w", err)
	}

	client := &DBClient{
		config: &dbConfig,
	}

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return client, nil
}

// Connect establishes connection to the database
func (c *DBClient) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Build the Data Source Name (DSN)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		c.config.User, c.config.Password, c.config.Host, c.config.Port, c.config.Name)

	// Open database connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		c.connected = false
		return fmt.Errorf("failed to open database connection: %w", err)
	}

	// Test the connection
	err = db.Ping()
	if err != nil {
		db.Close()
		c.connected = false
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Configure connection pool settings
	db.SetMaxOpenConns(25)   // Maximum number of open connections
	db.SetMaxIdleConns(25)   // Maximum number of idle connections
	db.SetConnMaxLifetime(0) // Maximum connection lifetime (0 = unlimited)

	c.db = db
	c.connected = true
	log.Printf("Successfully connected to MariaDB database: %s", c.config.Name)
	return nil
}

// Disconnect closes the database connection
func (c *DBClient) Disconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.db == nil {
		c.connected = false
		return nil
	}

	err := c.db.Close()
	c.connected = false
	return err
}

// isConnectionError checks if an error indicates a connection problem
func (c *DBClient) isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())
	return strings.Contains(errorMsg, "connection") ||
		strings.Contains(errorMsg, "broken pipe") ||
		strings.Contains(errorMsg, "network") ||
		strings.Contains(errorMsg, "timeout") ||
		strings.Contains(errorMsg, "eof") ||
		strings.Contains(errorMsg, "invalid connection") ||
		strings.Contains(errorMsg, "connection refused") ||
		strings.Contains(errorMsg, "server has gone away")
}

// reconnect attempts to reconnect to the database
func (c *DBClient) reconnect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Close existing connection if any
	if c.db != nil {
		c.db.Close()
	}
	c.connected = false

	// Wait a moment before retrying
	time.Sleep(100 * time.Millisecond)

	// Build the Data Source Name (DSN)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		c.config.User, c.config.Password, c.config.Host, c.config.Port, c.config.Name)

	// Open database connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to reconnect to database: %w", err)
	}

	// Test the connection
	err = db.Ping()
	if err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database after reconnection: %w", err)
	}

	// Configure connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(0)

	c.db = db
	c.connected = true
	return nil
}

// executeWithRetry executes a database operation with automatic retry on connection errors
func (c *DBClient) executeWithRetry(operation func(*sql.DB) error, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.mutex.RLock()
		if !c.connected && c.db != nil {
			c.mutex.RUnlock()
			if reconnectErr := c.reconnect(); reconnectErr != nil {
				lastErr = reconnectErr
				continue
			}
			c.mutex.RLock()
		}
		db := c.db
		c.mutex.RUnlock()

		if db == nil {
			lastErr = fmt.Errorf("no database connection available")
			if attempt < maxRetries {
				if reconnectErr := c.reconnect(); reconnectErr != nil {
					lastErr = reconnectErr
				}
			}
			continue
		}

		err := operation(db)
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

	return fmt.Errorf("database operation failed after %d retries, last error: %v", maxRetries+1, lastErr)
}

// DB returns the underlying sql.DB with retry mechanism
func (c *DBClient) DB() *sql.DB {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.db
}

// Exec executes a query with retry mechanism
func (c *DBClient) Exec(query string, args ...interface{}) (sql.Result, error) {
	var result sql.Result
	err := c.executeWithRetry(func(db *sql.DB) error {
		res, err := db.Exec(query, args...)
		if err != nil {
			return err
		}
		result = res
		return nil
	}, 2)
	return result, err
}

// Query executes a query that returns rows with retry mechanism
func (c *DBClient) Query(query string, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	err := c.executeWithRetry(func(db *sql.DB) error {
		res, err := db.Query(query, args...)
		if err != nil {
			return err
		}
		rows = res
		return nil
	}, 2)
	return rows, err
}

// QueryRow executes a query that returns at most one row with retry mechanism
func (c *DBClient) QueryRow(query string, args ...interface{}) *sql.Row {
	c.mutex.RLock()
	db := c.db
	c.mutex.RUnlock()

	if db == nil {
		// Return a row with an error that will be caught by Scan()
		return &sql.Row{}
	}

	return db.QueryRow(query, args...)
}

// Ping checks if the database connection is alive
func (c *DBClient) Ping() error {
	return c.executeWithRetry(func(db *sql.DB) error {
		return db.Ping()
	}, 2)
}

// IsConnected returns the current connection status
func (c *DBClient) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

// HealthCheck performs a simple query to verify the connection is working
func (c *DBClient) HealthCheck() error {
	return c.executeWithRetry(func(db *sql.DB) error {
		var result int
		return db.QueryRow("SELECT 1").Scan(&result)
	}, 2)
}

// Connect to the MariaDB database (legacy function for backward compatibility)
func InitDB() (*sql.DB, error) {
	client, err := NewDBClient()
	if err != nil {
		return nil, err
	}
	return client.DB(), nil
}

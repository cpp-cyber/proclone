package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

// DB is the global database connection
var DB *sql.DB

// Connect to the MariaDB database
func ConnectDB() (*sql.DB, error) {
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	// Check if any required environment variables are not set
	if dbHost == "" || dbPort == "" || dbUser == "" || dbName == "" || dbPassword == "" {
		return nil, fmt.Errorf("one or more database environment variables are not set")
	}

	// Build the Data Source Name (DSN)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	// Open database connection
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Test the connection
	err = db.Ping()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("Successfully connected to MariaDB database: %s", dbName)

	// Set the global DB variable
	DB = db

	return db, nil
}

// CloseDB closes the database connection
func CloseDB() error {
	if DB != nil {
		err := DB.Close()
		if err != nil {
			return fmt.Errorf("failed to close database connection: %w", err)
		}
		log.Println("Database connection closed")
	}
	return nil
}

// GetDB returns the global database connection
func GetDB() *sql.DB {
	return DB
}

// InitializeDB initializes the database connection and ensures it's ready for use
func InitializeDB() error {
	_, err := ConnectDB()
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Configure connection pool settings
	DB.SetMaxOpenConns(25)   // Maximum number of open connections
	DB.SetMaxIdleConns(25)   // Maximum number of idle connections
	DB.SetConnMaxLifetime(0) // Maximum connection lifetime (0 = unlimited)

	return nil
}

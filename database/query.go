package database

import (
	"database/sql"
	"fmt"
)

// Template represents a template record from the database
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ImagePath   string `json:"image_path"`
	Visible     bool   `json:"visible"`
	VMCount     int    `json:"vm_count"`
	Deployments int    `json:"deployments"`
	CreatedAt   string `json:"created_at"`
}

func BuildTemplates(rows *sql.Rows) ([]Template, error) {
	templates := []Template{}

	// Iterate through the result set
	for rows.Next() {
		var template Template
		err := rows.Scan(
			&template.Name,
			&template.Description,
			&template.ImagePath,
			&template.Visible,
			&template.VMCount,
			&template.Deployments,
			&template.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		templates = append(templates, template)
	}

	return templates, nil
}

// Returns all visible templates
func SelectVisibleTemplates() ([]Template, error) {
	if DB == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	var query = "SELECT * FROM templates WHERE visible = true"

	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query (%s): %w", query, err)
	}
	defer rows.Close()

	templates, err := BuildTemplates(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to build templates: %w", err)
	}

	return templates, nil
}

// Returns all templates
func SelectAllTemplates() ([]Template, error) {
	if DB == nil {
		return nil, fmt.Errorf("database connection is not initialized")
	}

	var query = "SELECT * FROM templates"

	rows, err := DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query (%s): %w", query, err)
	}
	defer rows.Close()

	templates, err := BuildTemplates(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to build templates: %w", err)
	}

	return templates, nil
}

// Insert template into database
func InsertTemplate(template Template) error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	query := "INSERT INTO templates (name, description, image_path, visible, vm_count) VALUES (?, ?, ?, ?, ?)"

	_, err := DB.Exec(query, template.Name, template.Description, template.ImagePath, template.Visible, template.VMCount)
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", query, err)
	}

	return nil
}

// Update template data
func UpdateTemplate(template Template) error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	query := "UPDATE templates SET description = ?, image_path = ?, visible = ?, vm_count = ? WHERE name = ?"

	_, err := DB.Exec(query, template.Description, template.ImagePath, template.Visible, template.VMCount, template.Name)
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", query, err)
	}

	return nil
}

// Helper function to select all template names
func SelectAllTemplateNames() ([]string, error) {
	templates, err := SelectAllTemplates()
	if err != nil {
		return nil, err
	}

	var templateNames []string
	for _, template := range templates {
		templateNames = append(templateNames, template.Name)
	}

	return templateNames, nil
}

// TODO: Implement
func AddDeployment(templateName string) error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	query := "UPDATE templates SET deployments = deployments + 1 WHERE name = ?"

	_, err := DB.Exec(query, templateName)
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", query, err)
	}

	return nil
}

// Toggles the visibility of a template
func ToggleVisibility(templateName string) error {
	if DB == nil {
		return fmt.Errorf("database connection is not initialized")
	}

	query := "UPDATE templates SET visible = NOT visible WHERE name = ?"

	_, err := DB.Exec(query, templateName)
	if err != nil {
		return fmt.Errorf("failed to execute query (%s): %w", query, err)
	}

	return nil
}

package cloning

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Utils

// buildTemplates builds template structs from SQL rows
func (c *TemplateClient) buildTemplates(rows *sql.Rows) ([]KaminoTemplate, error) {
	templates := []KaminoTemplate{}

	for rows.Next() {
		var template KaminoTemplate
		err := rows.Scan(
			&template.Name,
			&template.Description,
			&template.ImagePath,
			&template.TemplateVisible,
			&template.PodVisible,
			&template.VMsVisible,
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

// Use a map to define allowed MIME types for better performance
// and to avoid using a switch statement
var allowedMIMEs = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
}

// detectMIME reads a small buffer to determine the file's MIME type
func detectMIME(f multipart.File) (string, error) {
	buffer := make([]byte, 512)
	if _, err := f.Read(buffer); err != nil && err != io.EOF {
		return "", err
	}
	return http.DetectContentType(buffer), nil
}

// Database Operations

func (c *TemplateClient) GetTemplates() ([]KaminoTemplate, error) {
	query := "SELECT * FROM templates WHERE template_visible = true ORDER BY created_at DESC"
	rows, err := c.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return c.buildTemplates(rows)
}

func (c *TemplateClient) GetPublishedTemplates() ([]KaminoTemplate, error) {
	query := "SELECT * FROM templates"
	rows, err := c.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return c.buildTemplates(rows)
}

func (c *TemplateClient) DeleteTemplate(templateName string) error {
	// Get template image path and delete the image
	template, err := c.GetTemplateInfo(templateName)
	if err != nil {
		return fmt.Errorf("failed to get template info: %w", err)
	}

	// Only attempt to delete image if there's an image path
	if template.ImagePath != "" {
		err = c.DeleteImage(template.ImagePath)
		if err != nil {
			return fmt.Errorf("failed to delete template image: %w", err)
		}
	}

	//  Delete template from database
	query := "DELETE FROM templates WHERE name = ?"
	result, err := c.DB.Exec(query, templateName)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	// Check if any rows were affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("template not found: %s", templateName)
	}

	return nil
}

func (c *TemplateClient) ToggleTemplateVisibility(templateName string) error {
	query := "UPDATE templates SET template_visible = NOT template_visible WHERE name = ?"
	_, err := c.DB.Exec(query, templateName)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *TemplateClient) GetAllTemplateNames() ([]string, error) {
	templates, err := c.GetPublishedTemplates()
	if err != nil {
		return nil, err
	}

	var templateNames []string
	for _, template := range templates {
		templateNames = append(templateNames, template.Name)
	}

	return templateNames, nil
}

func (c *TemplateClient) InsertTemplate(template KaminoTemplate) error {
	query := "INSERT INTO templates (name, description, image_path, template_visible, vm_count) VALUES (?, ?, ?, ?, ?)"
	_, err := c.DB.Exec(query, template.Name, template.Description, template.ImagePath, template.TemplateVisible, template.VMCount)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *TemplateClient) UpdateTemplate(template KaminoTemplate) error {
	query := "UPDATE templates SET description = ?, image_path = ?, template_visible = ?, vm_count = ? WHERE name = ?"
	_, err := c.DB.Exec(query, template.Description, template.ImagePath, template.TemplateVisible, template.VMCount, template.Name)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *TemplateClient) AddDeployment(templateName string) error {
	query := "UPDATE templates SET deployments = deployments + 1 WHERE name = ?"
	_, err := c.DB.Exec(query, templateName)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *TemplateClient) GetTemplateInfo(templateName string) (KaminoTemplate, error) {
	query := "SELECT * FROM templates WHERE name = ?"
	row := c.DB.QueryRow(query, templateName)

	var template KaminoTemplate
	err := row.Scan(
		&template.Name,
		&template.Description,
		&template.ImagePath,
		&template.TemplateVisible,
		&template.PodVisible,
		&template.VMsVisible,
		&template.VMCount,
		&template.Deployments,
		&template.CreatedAt,
	)
	if err != nil {
		return KaminoTemplate{}, fmt.Errorf("failed to get template info: %w", err)
	}

	return template, nil
}

func (cm *CloningManager) GetUnpublishedTemplates() ([]string, error) {
	// Gets published templates from the database
	publishedTemplates, err := cm.DatabaseService.GetPublishedTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to get unpublished templates: %w", err)
	}

	// Gets pools that start with "kamino_template_" in Proxmox
	proxmoxTemplate, err := cm.ProxmoxService.GetTemplatePools()
	if err != nil {
		return nil, fmt.Errorf("failed to get Proxmox templates: %w", err)
	}

	var unpublished = []string{}
	for _, template := range proxmoxTemplate {
		trimmedTemplateName := strings.TrimPrefix(template, "kamino_template_")

		found := false
		for _, pubTemplate := range publishedTemplates {
			if pubTemplate.Name == trimmedTemplateName {
				found = true
				break
			}
		}

		if !found {
			unpublished = append(unpublished, trimmedTemplateName)
		}
	}

	return unpublished, nil
}

func (cm *CloningManager) PublishTemplate(template KaminoTemplate) error {
	// Insert template information into database
	if err := cm.DatabaseService.InsertTemplate(template); err != nil {
		return fmt.Errorf("failed to publish template: %w", err)
	}

	// Get all VMs in pool
	vms, err := cm.ProxmoxService.GetPoolVMs("kamino_template_" + template.Name)
	if err != nil {
		log.Printf("Error retrieving VMs in pool: %v", err)
		return fmt.Errorf("failed to get VMs in pool: %w", err)
	}

	// Convert all VMs to templates
	for _, vm := range vms {
		if err := cm.ProxmoxService.ConvertVMToTemplate(vm.NodeName, vm.VmId); err != nil {
			log.Printf("Error converting VM %d to template: %v", vm.VmId, err)
			return fmt.Errorf("failed to convert VM to template: %w", err)
		}
	}

	return nil
}

// Template Image Operations

func (cl *TemplateClient) UploadTemplateImage(c *gin.Context) (*UploadResult, error) {
	// Check header for multipart/form-data
	if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		return nil, fmt.Errorf("invalid content type")
	}

	// Parse the multipart form
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		return nil, fmt.Errorf("image field is required")
	}
	defer file.Close()

	// Basic check: Is file size 0?
	if header.Size == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}

	// Block unsupported file types
	filetype, err := detectMIME(file)
	if err != nil {
		return nil, fmt.Errorf("failed to detect file type")
	}
	if _, ok := allowedMIMEs[filetype]; !ok {
		return nil, fmt.Errorf("unsupported file type: %s", filetype)
	}

	// Reset file pointer back to beginning
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset file reader")
	}
	// File name sanitization
	filename := filepath.Base(header.Filename)        // basic sanitization
	filename = filepath.Clean(filename)               // clean up the filename
	filename = strings.ReplaceAll(filename, " ", "_") // replace spaces with underscores

	// Unique file name
	// Save with a UUID filename to avoid name collisions
	// generate unique filename
	newFilename := fmt.Sprintf("%s-%s", uuid.NewString(), filename)
	outPath := filepath.Join(cl.TemplateConfig.UploadDir, newFilename)

	// Save file using Gin utility
	if err := c.SaveUploadedFile(header, outPath); err != nil {
		return nil, fmt.Errorf("unable to save file: %w", err)
	}

	result := &UploadResult{
		Message:  "file uploaded successfully",
		Filename: newFilename,
		MimeType: filetype,
		Path:     outPath,
	}

	return result, nil
}

func (c *TemplateClient) DeleteImage(imagePath string) error {
	if imagePath == "" {
		return fmt.Errorf("image path is empty")
	}

	fullPath := filepath.Join(c.TemplateConfig.UploadDir, imagePath)
	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("failed to delete image: %w", err)
	}
	return nil
}

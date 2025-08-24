package images

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var UploadDir = os.Getenv("UPLOADS_DIR")

// Use a map to define allowed MIME types for better performance
// and to avoid using a switch statement
var allowedMIMEs = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
}

func HandleUpload(c *gin.Context) {
	session := sessions.Default(c)
	isAdmin := session.Get("is_admin")

	// Make sure user is admin (redundant)
	if !isAdmin.(bool) {
		log.Printf("Forbidden access attempt")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Only Admin users can upload a template image",
		})
		return
	}

	// Check header for multipart/form-data
	if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Content-Type must be multipart/form-data"})
		return
	}

	// Parse the multipart form
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image field is required"})
		return
	}
	defer file.Close()

	// Basic check: Is file size 0?
	if header.Size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "uploaded file is empty"})
		return
	}

	// Block unsupported file types
	filetype, err := detectMIME(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to detect file type"})
		return
	}
	if _, ok := allowedMIMEs[filetype]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type", "type": filetype})
		return
	}

	// Reset file pointer back to beginning
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reset file reader"})
		return
	}
	// File name sanitization
	filename := filepath.Base(header.Filename)        // basic sanitization
	filename = filepath.Clean(filename)               // clean up the filename
	filename = strings.ReplaceAll(filename, " ", "_") // replace spaces with underscores

	// Unique file name
	// Save with a UUID filename to avoid name collisions
	// generate unique filename
	newFilename := fmt.Sprintf("%s-%s", uuid.NewString(), filename)
	outPath := filepath.Join(UploadDir, newFilename)

	// Save file using Gin utility
	if err := c.SaveUploadedFile(header, outPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to save file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "file uploaded successfully",
		"filename":  newFilename,
		"mime_type": filetype,
		"path":      outPath,
	})
}

// detectMIME reads a small buffer to determine the file's MIME type
func detectMIME(f multipart.File) (string, error) {
	buffer := make([]byte, 512)
	if _, err := f.Read(buffer); err != nil && err != io.EOF {
		return "", err
	}
	return http.DetectContentType(buffer), nil
}

func HandleGetFile(c *gin.Context) {
	filename := c.Param("filename")
	filePath := filepath.Join(UploadDir, filename)

	// Serve the file
	c.File(filePath)
}

package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ProxmoxAPIRequest represents a request to the Proxmox API
type ProxmoxAPIRequest struct {
	Method      string // GET, POST, PUT, DELETE
	Endpoint    string // The API endpoint (e.g., "/nodes", "/nodes/node1/status")
	RequestBody any    // Optional request body for POST/PUT requests
}

// ProxmoxAPIResponse represents the generic Proxmox API response structure
type ProxmoxAPIResponse struct {
	Data json.RawMessage `json:"data"`
}

// ProxmoxRequestHelper provides a helper for making HTTP requests to Proxmox API
type ProxmoxRequestHelper struct {
	BaseURL    string
	APIToken   string
	HTTPClient *http.Client
}

// NewProxmoxRequestHelper creates a new Proxmox request helper
func NewProxmoxRequestHelper(baseURL, apiToken string, httpClient *http.Client) *ProxmoxRequestHelper {
	return &ProxmoxRequestHelper{
		BaseURL:    baseURL,
		APIToken:   apiToken,
		HTTPClient: httpClient,
	}
}

// MakeRequest performs an HTTP request to the Proxmox API and returns the raw response data
func (prh *ProxmoxRequestHelper) MakeRequest(req ProxmoxAPIRequest) (json.RawMessage, error) {
	var reqBody io.Reader

	// Prepare request body for POST/PUT requests
	if req.Method == "POST" || req.Method == "PUT" {
		var bodyData any
		if req.RequestBody != nil {
			bodyData = req.RequestBody
		} else {
			bodyData = map[string]any{}
		}

		jsonData, err := json.Marshal(bodyData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	// Create the full URL
	url := prh.BaseURL + req.Endpoint

	// Create HTTP request
	httpReq, err := http.NewRequest(req.Method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s request to %s: %w", req.Method, req.Endpoint, err)
	}

	// Set headers
	httpReq.Header.Add("Authorization", "PVEAPIToken="+prh.APIToken)
	httpReq.Header.Add("Content-Type", "application/json")

	// Execute the request
	resp, err := prh.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute %s request to %s: %w", req.Method, req.Endpoint, err)
	}
	defer resp.Body.Close()

	// Read response body first for better error reporting
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s %s: %w", req.Method, req.Endpoint, err)
	}

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("proxmox API returned status %d for %s %s, response: %s", resp.StatusCode, req.Method, req.Endpoint, string(bodyBytes))
	}

	// Don't try to parse into ProxmoxAPIResponse structure for DELETE operations
	if req.Method == "DELETE" {
		return json.RawMessage("nil"), nil
	}

	// Decode the API response for other methods
	var apiResponse ProxmoxAPIResponse
	if err := json.Unmarshal(bodyBytes, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response from %s %s: %w", req.Method, req.Endpoint, err)
	}

	return apiResponse.Data, nil
}

// MakeRequestAndUnmarshal performs an HTTP request and unmarshals the response into the provided interface
func (prh *ProxmoxRequestHelper) MakeRequestAndUnmarshal(req ProxmoxAPIRequest, target any) error {
	data, err := prh.MakeRequest(req)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal response data from %s %s: %w", req.Method, req.Endpoint, err)
	}

	return nil
}

package proxmox

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
)

// kind should be "GET", "DELETE", "POST", or "PUT", jsonData and httpClient can be nil
func MakeRequest(config *ProxmoxConfig, path string, kind string, jsonData []byte, httpClient *http.Client) (int, []byte, error) {
	if !(kind == "GET" || kind == "DELETE" || kind == "POST" || kind == "PUT") {
		return 0, nil, fmt.Errorf("invalid REST method passed: %s", kind)
	}

	var client *http.Client = nil
	if httpClient == nil {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.VerifySSL},
		}
		client = &http.Client{Transport: tr}
	} else {
		client = httpClient
	}

	reqURL := fmt.Sprintf("https://%s:%s/%s", config.Host, config.Port, path)

	var bodyReader io.Reader = nil

	if kind != "GET" && kind != "DELETE" && jsonData != nil {
		bodyReader = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(kind, reqURL, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create %s request: %v", kind, err)
	}

	if kind != "GET" && kind != "DELETE" && jsonData != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
	}

	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s", config.APIToken))

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return resp.StatusCode, body, nil
}

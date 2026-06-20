package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// loadAuth loads config and credentials in one call to reduce repetition.
func LoadAuth() (*AgentConfig, *AgentCredentials, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, nil, err
	}
	creds, err := LoadCredentials()
	if err != nil {
		return nil, nil, err
	}
	return cfg, creds, nil
}

// apiDo sends an authenticated HTTP request to the agentlink server.
//
// The caller MUST close resp.Body when done.
func APIDo(cfg *AgentConfig, creds *AgentCredentials, method, url string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("cannot encode request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, cfg.Server+url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server %s: %w", cfg.Server, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return resp, nil
}

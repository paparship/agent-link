package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RunPing() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	url := cfg.Server + "/agents/heartbeat"
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot connect to server %s: %w", cfg.Server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	fmt.Println("✓ Heartbeat sent")
	return nil
}

type agentInfo struct {
	Device   string   `json:"device"`
	Sessions []string `json:"sessions"`
	LastSeen string   `json:"last_seen"`
	Online   bool     `json:"online"`
}

type agentListResponse struct {
	Agents []agentInfo `json:"agents"`
}

func RunList(all bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	url := cfg.Server + "/agents/list"
	if all {
		url += "?all=true"
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot connect to server %s: %w", cfg.Server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var list agentListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fmt.Errorf("cannot parse response: %w", err)
	}

	for i, a := range list.Agents {
		if i > 0 {
			fmt.Println("---")
		}
		status := "offline"
		if a.Online {
			status = "online"
		}
		sessions := strings.Join(a.Sessions, ", ")
		if sessions == "" {
			sessions = "(none)"
		}
		fmt.Printf("Device:     %s\n", a.Device)
		fmt.Printf("Sessions:   %s\n", sessions)
		fmt.Printf("Status:     %s\n", status)
		if a.LastSeen != "" {
			fmt.Printf("Last seen:  %s\n", a.LastSeen)
		}
	}

	return nil
}

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RunPing() error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	resp, err := apiDo(cfg, creds, "POST", "/agents/heartbeat", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

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
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	path := "/agents/list"
	if all {
		path += "?all=true"
	}

	resp, err := apiDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RunSend(target, content string) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	session, err := findCurrentSession()
	if err != nil {
		return err
	}

	if !strings.Contains(target, ":") {
		target = cfg.Device + ":" + target
	}

	resp, err := apiDo(cfg, creds, "POST", "/messages/send", map[string]string{
		"to":           target,
		"from_session": session,
		"content":      content,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()

	fmt.Println("✓ Message sent")
	return nil
}

func RunPull(all bool) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	session, err := findCurrentSession()
	if err != nil {
		return err
	}

	limit := 1
	if all {
		limit = 10
	}

	path := fmt.Sprintf("/inbox/pull?session=%s&limit=%d", session, limit)
	resp, err := apiDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			Type        string `json:"type"`
			FromDevice  string `json:"from_device"`
			FromSession string `json:"from_session"`
			Content     string `json:"content"`
			CreatedAt   string `json:"created_at"`
			TaskID      string `json:"task_id,omitempty"`
		} `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Items) == 0 {
		fmt.Println("No messages")
		return nil
	}

	for _, msg := range result.Items {
		fmt.Printf("[%s] from %s:%s — %s\n", msg.Type, msg.FromDevice, msg.FromSession, msg.CreatedAt)
		if msg.Type == "task" && msg.TaskID != "" {
			fmt.Printf("  Task ID: %s\n", msg.TaskID)
		}
		fmt.Println(msg.Content)
		fmt.Println("---")
	}

	return nil
}

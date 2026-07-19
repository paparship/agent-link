package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

type recipientStatusJSON struct {
	Device  string `json:"device"`
	Session string `json:"session"`
	Current string `json:"current"`
}

func displayRecipientStatus(status *recipientStatusJSON) {
	if status == nil {
		return
	}
	if status.Device == "" && status.Session == "" {
		return
	}
	fmt.Printf("\n%s %s session status: %s\n", status.Device, status.Session, status.Current)
}

func RunSend(target, content string, interrupt bool, title string) error {
	cfg, creds, err := LoadAuth()
	if err != nil {
		return err
	}

	session, err := FindCurrentSession()
	if err != nil {
		return err
	}

	if !strings.Contains(target, ":") {
		target = cfg.Device + ":" + target
	}

	resp, err := APIDo(cfg, creds, "POST", "/messages/send", map[string]any{
		"to":           target,
		"from_session": session,
		"interrupt":    interrupt,
		"title":        title,
		"content":      content,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		ID              string               `json:"id"`
		RecipientStatus *recipientStatusJSON `json:"recipient_status,omitempty"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("✓ message delivered (id: %s)\n", result.ID)
	displayRecipientStatus(result.RecipientStatus)

	return nil
}

func RunPull(all bool) error {
	cfg, creds, err := LoadAuth()
	if err != nil {
		return err
	}

	session, err := FindCurrentSession()
	if err != nil {
		return err
	}

	limit := 1
	if all {
		limit = 10
	}

	path := fmt.Sprintf("/inbox/pull?session=%s&limit=%d", session, limit)
	resp, err := APIDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			ID          string `json:"id"`
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
		fmt.Printf("[%s] %s from %s:%s — %s\n", msg.Type, msg.ID, msg.FromDevice, msg.FromSession, msg.CreatedAt)
		if msg.Type == "task" && msg.TaskID != "" {
			fmt.Printf("  Task ID: %s\n", msg.TaskID)
		}
		fmt.Println(msg.Content)
		fmt.Println("---")
	}

	return nil
}

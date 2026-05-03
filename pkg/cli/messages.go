package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RunSend(target, content string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
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

	body := map[string]string{
		"to":           target,
		"from_session": session,
		"content":      content,
	}
	data, _ := json.Marshal(body)

	url := cfg.Server + "/messages/send"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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

	fmt.Println("✓ Message sent")
	return nil
}

func RunPull(all bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
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

	url := fmt.Sprintf("%s/inbox/pull?session=%s&limit=%d", cfg.Server, session, limit)
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

	var result struct {
		Items []struct {
			Type        string `json:"type"`
			FromDevice  string `json:"from_device"`
			FromSession string `json:"from_session"`
			Content     string `json:"content"`
			CreatedAt   string `json:"created_at"`
		} `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Items) == 0 {
		fmt.Println("No messages")
		return nil
	}

	for _, msg := range result.Items {
		fmt.Printf("[%s] from %s:%s — %s\n", msg.Type, msg.FromDevice, msg.FromSession, msg.CreatedAt)
		fmt.Println(msg.Content)
		fmt.Println("---")
	}

	return nil
}

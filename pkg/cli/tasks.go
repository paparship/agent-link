package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RunTaskSend(target, taskID, content string) error {
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
		"task_id":      taskID,
		"content":      content,
	}
	data, _ := json.Marshal(body)

	url := cfg.Server + "/tasks/send"
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

	fmt.Printf("✓ Task %s sent to %s\n", taskID, target)
	return nil
}

func RunTaskResult(taskID, status, result string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	body := map[string]string{
		"task_id": taskID,
		"status":  status,
		"result":  result,
	}
	data, _ := json.Marshal(body)

	url := cfg.Server + "/tasks/result"
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

	verb := "completed"
	if status == "suspended" {
		verb = "suspended"
	}
	fmt.Printf("✓ Task %s %s\n", taskID, verb)
	return nil
}

func RunTaskResume(taskID, content string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	body := map[string]string{
		"task_id": taskID,
		"content": content,
	}
	data, _ := json.Marshal(body)

	url := cfg.Server + "/tasks/resume"
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

	fmt.Printf("✓ Task %s resumed\n", taskID)
	return nil
}

func RunTaskCancel(taskID string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	body := map[string]string{
		"task_id": taskID,
	}
	data, _ := json.Marshal(body)

	url := cfg.Server + "/tasks/cancel"
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

	fmt.Printf("✓ Task %s cancelled\n", taskID)
	return nil
}

func RunTaskStatus(taskID string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/tasks/status?task_id=%s", cfg.Server, taskID)
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

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("Task %s not found", taskID)
	}

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
		TaskID      string `json:"task_id"`
		Status      string `json:"status"`
		AssignedTo  string `json:"assigned_to"`
		IssuedBy    string `json:"issued_by"`
		Content     string `json:"content"`
		Result      string `json:"result"`
		IssuedAt    string `json:"issued_at"`
		CompletedAt string `json:"completed_at"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Task:      %s\n", result.TaskID)
	fmt.Printf("Status:    %s\n", result.Status)
	fmt.Printf("Assigned:  %s\n", result.AssignedTo)
	fmt.Printf("Issued by: %s\n", result.IssuedBy)
	fmt.Printf("Content:   %s\n", result.Content)
	if result.Result != "" {
		fmt.Printf("Result:    %s\n", result.Result)
	}
	fmt.Printf("Issued:    %s\n", result.IssuedAt)
	if result.CompletedAt != "" {
		fmt.Printf("Completed: %s\n", result.CompletedAt)
	}

	return nil
}

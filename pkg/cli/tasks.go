package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RunTaskSend(target, taskID, content string, interrupt bool) error {
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

	body, _ := json.Marshal(map[string]any{
		"to":           target,
		"from_session": session,
		"task_id":      taskID,
		"content":      content,
		"interrupt":    interrupt,
	})

	req, err := http.NewRequest("POST", cfg.Server+"/tasks/send", bytes.NewReader(body))
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

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusConflict {
		var errResp struct {
			Error           string               `json:"error"`
			RecipientStatus *recipientStatusJSON `json:"recipient_status,omitempty"`
		}
		json.Unmarshal(respBody, &errResp)

		fmt.Printf("✗ %s\n", errResp.Error)
		displayRecipientStatus(errResp.RecipientStatus)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &e) == nil && e.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, e.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var sendResp struct {
		ID              string               `json:"id"`
		TaskID          string               `json:"task_id,omitempty"`
		RecipientStatus *recipientStatusJSON `json:"recipient_status,omitempty"`
	}
	json.Unmarshal(respBody, &sendResp)
	if sendResp.TaskID != "" {
		taskID = sendResp.TaskID
	}

	fmt.Printf("✓ Task %s sent to %s\n", taskID, target)
	displayRecipientStatus(sendResp.RecipientStatus)

	return nil
}

func RunTaskResult(taskID, status, result string) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	resp, err := apiDo(cfg, creds, "POST", "/tasks/result", map[string]string{
		"task_id": taskID,
		"status":  status,
		"result":  result,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()

	verb := "completed"
	if status == "suspended" {
		verb = "suspended"
	}
	fmt.Printf("✓ Task %s %s\n", taskID, verb)
	return nil
}

func RunTaskResume(taskID, content string) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	resp, err := apiDo(cfg, creds, "POST", "/tasks/resume", map[string]string{
		"task_id": taskID,
		"content": content,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()

	fmt.Printf("✓ Task %s resumed\n", taskID)
	return nil
}

func RunTaskCancel(taskID string) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	resp, err := apiDo(cfg, creds, "POST", "/tasks/cancel", map[string]string{
		"task_id": taskID,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()

	fmt.Printf("✓ Task %s cancelled\n", taskID)
	return nil
}

func RunTaskList() error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	session, err := findCurrentSession()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/tasks/list?session=%s", session)
	resp, err := apiDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		Tasks []struct {
			TaskID      string `json:"task_id"`
			Status      string `json:"status"`
			AssignedTo  string `json:"assigned_to"`
			IssuedBy    string `json:"issued_by"`
			Content     string `json:"content"`
			Result      string `json:"result"`
			IssuedAt    string `json:"issued_at"`
			CompletedAt string `json:"completed_at"`
		} `json:"tasks"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Tasks) == 0 {
		fmt.Println("No active tasks")
		return nil
	}

	for _, t := range result.Tasks {
		fmt.Printf("Task:      %s\n", t.TaskID)
		fmt.Printf("Status:    %s\n", t.Status)
		fmt.Printf("Assigned:  %s\n", t.AssignedTo)
		fmt.Printf("Issued by: %s\n", t.IssuedBy)
		fmt.Printf("Content:   %s\n", t.Content)
		if t.Result != "" {
			fmt.Printf("Result:    %s\n", t.Result)
		}
		fmt.Printf("Issued:    %s\n", t.IssuedAt)
		if t.CompletedAt != "" {
			fmt.Printf("Completed: %s\n", t.CompletedAt)
		}
		fmt.Println("---")
	}

	return nil
}

func RunTaskStatus(taskID string) error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/tasks/status?task_id=%s", taskID)
	resp, err := apiDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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

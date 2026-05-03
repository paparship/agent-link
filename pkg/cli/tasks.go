package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RunTaskSend(target, taskID, content string) error {
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

	resp, err := apiDo(cfg, creds, "POST", "/tasks/send", map[string]string{
		"to":           target,
		"from_session": session,
		"task_id":      taskID,
		"content":      content,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()

	fmt.Printf("✓ Task %s sent to %s\n", taskID, target)
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

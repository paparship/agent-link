package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

type recipientStatusJSON struct {
	Device       string `json:"device"`
	Session      string `json:"session"`
	Status       string `json:"status"`
	CurrentTask  string `json:"current_task,omitempty"`
	TaskDuration string `json:"task_duration,omitempty"`
	InboxDepth   int    `json:"inbox_depth"`
	LastSeen     string `json:"last_seen,omitempty"`
}

func displayRecipientStatus(status *recipientStatusJSON) {
	if status == nil {
		return
	}
	device := status.Device
	session := status.Session
	if device == "" && session == "" {
		return
	}

	switch status.Status {
	case "busy":
		taskInfo := "处理 task #" + status.CurrentTask
		if status.TaskDuration != "" {
			taskInfo += "（已进行 " + status.TaskDuration + "）"
		}
		fmt.Printf("\n%s %s session 当前状态:\n", device, session)
		fmt.Printf("  忙碌 — %s\n", taskInfo)
	case "idle":
		fmt.Printf("\n%s %s session 当前状态: 空闲\n", device, session)
	case "offline":
		fmt.Printf("\n%s %s session 当前离线\n", device, session)
		if status.LastSeen != "" {
			fmt.Printf("  最后在线: %s\n", status.LastSeen)
		}
	}
	if status.InboxDepth > 1 {
		fmt.Printf("  未读: %d 条\n", status.InboxDepth)
	}
}

func RunSend(target, content string, interrupt bool) error {
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

	fmt.Printf("✓ 消息已投递（ID: %s）\n", result.ID)
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

func RunMessageStatus(msgID string) error {
	if msgID == "" {
		return fmt.Errorf("message ID is required")
	}

	cfg, creds, err := LoadAuth()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/messages/status?id=%s", msgID)
	resp, err := APIDo(cfg, creds, "GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		ID          string `json:"id"`
		FromDevice  string `json:"from_device"`
		FromSession string `json:"from_session"`
		ToDevice    string `json:"to_device"`
		ToSession   string `json:"to_session"`
		Content     string `json:"content"`
		Type        string `json:"type"`
		TaskID      string `json:"task_id,omitempty"`
		Status      string `json:"status"`
		SentAt      string `json:"sent_at"`
		DeliveredAt string `json:"delivered_at,omitempty"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("消息:      %s\n", result.ID)
	fmt.Printf("发送:      %s:%s → %s:%s\n", result.FromDevice, result.FromSession, result.ToDevice, result.ToSession)
	fmt.Printf("内容:      %s\n", result.Content)
	fmt.Printf("时间:      %s\n", result.SentAt)
	fmt.Printf("状态:      %s\n", result.Status)
	if result.DeliveredAt != "" {
		fmt.Printf("已读取:    %s\n", result.DeliveredAt)
	}
	if result.TaskID != "" {
		fmt.Printf("Task ID:   %s\n", result.TaskID)
	}

	return nil
}

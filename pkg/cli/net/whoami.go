package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type taskItem struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	AssignedTo  string `json:"assigned_to"`
	IssuedBy    string `json:"issued_by"`
	Content     string `json:"content"`
	Result      string `json:"result,omitempty"`
	IssuedAt    string `json:"issued_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type whoamiResponse struct {
	Device        string            `json:"device"`
	Session       string            `json:"session"`
	Current       string            `json:"current"`
	Inbox         map[string]int    `json:"inbox"`
	ReceivedTasks []taskItem        `json:"received_tasks"`
	SentTasks     []taskItem        `json:"sent_tasks"`
}

func RunWhoami() error {
	cfg, creds, err := LoadAuth()
	if err != nil {
		return err
	}

	session, err := FindCurrentSession()
	if err != nil {
		return err
	}

	resp, err := APIDo(cfg, creds, "GET", "/whoami?session="+session, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct{ Error string }
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("whoami: %s", errResp.Error)
		}
		return fmt.Errorf("whoami: HTTP %d", resp.StatusCode)
	}

	var w whoamiResponse
	json.NewDecoder(resp.Body).Decode(&w)

	fmt.Printf("You are %s:%s\n", w.Device, w.Session)
	fmt.Printf("Current: %s\n", w.Current)
	fmt.Printf("Inbox: %d received, %d sent\n", w.Inbox["received"], w.Inbox["sent"])

	if len(w.ReceivedTasks) > 0 {
		fmt.Printf("\nReceived tasks:\n")
		for _, t := range w.ReceivedTasks {
			extra := ""
			if t.CompletedAt != "" {
				extra += " (completed)"
			}
			if t.Result != "" {
				extra += " → " + t.Result
			}
			fmt.Printf("  %-12s  %-15s  %s%s\n", t.TaskID, t.Status, t.Content, extra)
		}
	}

	if len(w.SentTasks) > 0 {
		fmt.Printf("\nSent tasks:\n")
		for _, t := range w.SentTasks {
			target := t.AssignedTo
			extra := ""
			if t.CompletedAt != "" {
				extra += " (completed)"
			}
			if t.Result != "" {
				extra += " → " + t.Result
			}
			fmt.Printf("  %-12s  %-15s  %s%s  → %s\n", t.TaskID, t.Status, t.Content, extra, target)
		}
	}

	fmt.Println("\nTask vs Msg:")
	fmt.Println("  task — need a result back (completed/suspended)")
	fmt.Println("  msg  — fire-and-forget, no reply needed")
	fmt.Println()
	fmt.Println("Commands (for agent use):")
	fmt.Println("  agentlink task send <target> <id> \"<content>\"      — issue task (need result)")
	fmt.Println("  agentlink task result <id> completed \"<msg>\"        — report result (worker)")
	fmt.Println("  agentlink task status <id>                           — task detail")
	fmt.Println("  agentlink task list                                  — full task list")
	fmt.Println("  agentlink session add <name>                         — create new agent session")
	fmt.Println("  agentlink session remove <name>                      — remove agent session")
	fmt.Println("  agentlink list --all                                 — team devices")
	fmt.Println("  agentlink send [--interrupt] <target> \"<msg>\"       — send msg (no reply)")
	fmt.Println()
	fmt.Println("User-only (do NOT run): init, install, uninstall, restart, attach")
	return nil
}

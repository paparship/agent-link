package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

func RunSessionAdd(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	if err := checkTmux(); err != nil {
		return err
	}

	sessionDir := filepath.Join(cfg.BaseDir, name)
	if _, err := os.Stat(sessionDir); err == nil {
		return fmt.Errorf("session directory %q already exists", sessionDir)
	}

	// Fetch current sessions from server
	currentSessions, err := fetchSessions(cfg.Server, creds.APIKey)
	if err != nil {
		return err
	}

	// Check for duplicate
	for _, s := range currentSessions {
		if s == name {
			return fmt.Errorf("session %q already registered on device", name)
		}
	}

	updatedSessions := append(currentSessions, name)

	// Call PATCH /agents/sessions
	if err := patchSessions(cfg.Server, creds.APIKey, updatedSessions); err != nil {
		return err
	}

	// Create local directory
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", sessionDir, err)
	}

	// Write .agentlink.toml
	tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
	if err := writeSessionTOML(tomlPath, name, cfg.Device); err != nil {
		return fmt.Errorf("cannot write %s: %w", tomlPath, err)
	}

	// Write CLAUDE.md
	claudePath := filepath.Join(sessionDir, "CLAUDE.md")
	claudeContent := claudeMDContent(name)
	if err := os.WriteFile(claudePath, []byte(claudeContent), 0600); err != nil {
		return fmt.Errorf("cannot write %s: %w", claudePath, err)
	}

	fmt.Printf("✓ Session %q added\n", name)
	fmt.Printf("  Directory: %s\n", sessionDir)
	fmt.Println()
	fmt.Printf("Next step:\n")
	fmt.Printf("  agentlink attach %s    # enter the session\n", name)

	return nil
}

func RunSessionRemove(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	// Fetch current sessions from server
	currentSessions, err := fetchSessions(cfg.Server, creds.APIKey)
	if err != nil {
		return err
	}

	// Check session exists
	found := false
	for _, s := range currentSessions {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session %q not found on device", name)
	}

	if len(currentSessions) <= 1 {
		return fmt.Errorf("cannot remove the last session; use 'agentlink device remove' instead")
	}

	// Remove from list
	var updatedSessions []string
	for _, s := range currentSessions {
		if s != name {
			updatedSessions = append(updatedSessions, s)
		}
	}

	// Call PATCH /agents/sessions
	if err := patchSessions(cfg.Server, creds.APIKey, updatedSessions); err != nil {
		return err
	}

	// Remove local directory
	sessionDir := filepath.Join(cfg.BaseDir, name)
	if err := os.RemoveAll(sessionDir); err != nil {
		return fmt.Errorf("warning: session removed from server but could not remove %s: %w", sessionDir, err)
	}

	fmt.Printf("✓ Session %q removed\n", name)
	return nil
}

func RunDeviceRemove() error {
	// Load config and credentials — they may not exist if already cleaned up,
	// but we need them for the API call
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	creds, err := loadCredentials()
	if err != nil {
		return err
	}

	// Call DELETE /agents/device
	url := cfg.Server + "/agents/device"
	req, err := http.NewRequest("DELETE", url, nil)
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
		var errResp struct{ Error string `json:"error"` }
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	// Clean up local .agentlink directory
	agentlinkDir := filepath.Join(os.Getenv("HOME"), ".agentlink")
	if err := os.RemoveAll(agentlinkDir); err != nil {
		return fmt.Errorf("warning: device unregistered from server but could not remove %s: %w", agentlinkDir, err)
	}

	fmt.Println("✓ Device unregistered")
	return nil
}

func RunAttach(session string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// Check tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required but not found in PATH")
	}

	// Check directory exists
	sessionDir := filepath.Join(cfg.BaseDir, session)
	if _, err := os.Stat(sessionDir); err != nil {
		return fmt.Errorf("session directory %q not found; use 'agentlink session add %s' first", sessionDir, session)
	}

	// Check if tmux session exists
	hasSession := exec.Command("tmux", "has-session", "-t", session).Run() == nil

	if hasSession {
		cmd := exec.Command("tmux", "attach", "-t", session)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("tmux", "new-session", "-c", sessionDir, "claude", "--dangerously-skip-permissions")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func claudeMDContent(session string) string {
	bt := "`"

	switch session {
	case "main":
		return "# main session\n\n## 通信\n\n" +
			"- " + bt + `agentlink task send <target> <task_id> "<content>"` + bt + " — 发放任务\n" +
			"- " + bt + `agentlink task resume <task_id> "<guidance>"` + bt + " — 恢复挂起任务\n" +
			"- " + bt + "agentlink task cancel <task_id>" + bt + " — 取消任务\n" +
			"- " + bt + "agentlink pull" + bt + " — 拉取回报\n" +
			"- " + bt + "agentlink list --all" + bt + " — 查看所有设备状态\n"
	case "worker":
		return "# worker session\n\n## 通信\n\n" +
			"- " + bt + "agentlink pull" + bt + " — 拉取任务或消息\n" +
			"- " + bt + `agentlink task result <task_id> completed "<result>"` + bt + " — 回报完成\n" +
			"- " + bt + `agentlink task result <task_id> suspended "<reason>"` + bt + " — 回报挂起\n" +
			"- " + bt + `agentlink send <target> "<content>"` + bt + " — 发送消息\n"
	default:
		return "# " + session + " session\n\n## 通信\n\n" +
			"- " + bt + "agentlink pull" + bt + " — 拉取任务或消息\n" +
			"- " + bt + `agentlink send <target> "<content>"` + bt + " — 发送消息\n"
	}
}

// -- helpers --

func checkTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required but not found in PATH")
	}
	return nil
}

func fetchSessions(server, apiKey string) ([]string, error) {
	url := server + "/agents/list"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server %s: %w", server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct{ Error string `json:"error"` }
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var list struct {
		Agents []struct {
			Sessions []string `json:"sessions"`
		} `json:"agents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("cannot parse response: %w", err)
	}

	if len(list.Agents) == 0 {
		return nil, fmt.Errorf("device not found on server")
	}

	return list.Agents[0].Sessions, nil
}

func patchSessions(server, apiKey string, sessions []string) error {
	body := map[string][]string{"sessions": sessions}
	data, _ := json.Marshal(body)

	url := server + "/agents/sessions"
	req, err := http.NewRequest("PATCH", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot connect to server %s: %w", server, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		var errResp struct{ Error string `json:"error"` }
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	return nil
}

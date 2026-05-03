package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/team/agentlink/pkg/adapter"
)

func RunSessionAdd(name string) error {
	cfg, creds, err := loadAuth()
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
	currentSessions, err := fetchSessions(cfg, creds)
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
	if err := patchSessions(cfg, creds, updatedSessions); err != nil {
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
	launcher := adapter.NewLauncher(cfg.Agent)
	claudeContent := launcher.InitTemplate(name)
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
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	// Fetch current sessions from server
	currentSessions, err := fetchSessions(cfg, creds)
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
	if err := patchSessions(cfg, creds, updatedSessions); err != nil {
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
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	// Call DELETE /agents/device
	resp, err := apiDo(cfg, creds, "DELETE", "/agents/device", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

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

	launcher := adapter.NewLauncher(cfg.Agent)
	name, args := launcher.Command()
	cmdArgs := append([]string{"new-session", "-c", sessionDir, name}, args...)
	cmd := exec.Command("tmux", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// -- helpers --

func checkTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required but not found in PATH")
	}
	return nil
}

func fetchSessions(cfg *AgentConfig, creds *AgentCredentials) ([]string, error) {
	resp, err := apiDo(cfg, creds, "GET", "/agents/list", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

func patchSessions(cfg *AgentConfig, creds *AgentCredentials, sessions []string) error {
	resp, err := apiDo(cfg, creds, "PATCH", "/agents/sessions", map[string][]string{
		"sessions": sessions,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

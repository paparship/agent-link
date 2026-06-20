package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// RunResume rebuilds tmux sessions and pollers from the on-disk config,
// without re-registering the device. Each session's Claude Code is resumed
// to its recorded session_id (from [sessions]); configs without [sessions]
// fall back to --continue (23c).
func RunResume() error {
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

	// Verify the device is still registered on the server. If it was
	// uninstalled, resume cannot proceed — the user must re-init.
	if err := pingServer(cfg, creds); err != nil {
		return fmt.Errorf("device check failed (was it uninstalled?): %w", err)
	}

	// Determine which sessions to resume. With [sessions], use those keys;
	// without (legacy config), scan BaseDir for session directories (23c).
	sessionNames, fallback := resumeSessionList(cfg)
	if len(sessionNames) == 0 {
		return fmt.Errorf("no sessions found under %s; run `agentlink init` first", cfg.BaseDir)
	}

	if fallback {
		fmt.Println("⚠ config.toml has no [sessions] segment — using --continue fallback")
		fmt.Println("  Run `agentlink init` again to enable precise session resume")
	}

	// Launch tmux sessions with Resume=true. launchSessions reads
	// opts.Existing to decide --resume <id> vs --continue per session.
	if _, err := launchSessions(cfg.BaseDir, cfg.Agent, launchOpts{
		Resume:   true,
		NoPoll:   !cfg.Poll.Enabled,
		Existing: cfg.Sessions,
	}); err != nil {
		return err
	}

	// Send a heartbeat so the device shows online immediately.
	if err := RunPing(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: heartbeat failed: %v\n", err)
	}

	fmt.Println("✓ sessions resumed")
	for _, name := range sessionNames {
		fmt.Printf("  %s — attach with: agentlink attach %s\n", name, name)
	}
	return nil
}

// resumeSessionList returns the session names to resume and whether the
// [sessions] segment was absent (triggering 23c fallback).
func resumeSessionList(cfg *AgentConfig) ([]string, bool) {
	if len(cfg.Sessions) > 0 {
		names := make([]string, 0, len(cfg.Sessions))
		for k := range cfg.Sessions {
			names = append(names, k)
		}
		sort.Strings(names)
		return names, false
	}
	// Legacy config: scan BaseDir for directories containing .agentlink.toml
	var names []string
	entries, err := os.ReadDir(cfg.BaseDir)
	if err != nil {
		return nil, true
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tomlPath := filepath.Join(cfg.BaseDir, e.Name(), ".agentlink.toml")
		if _, err := os.Stat(tomlPath); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, true
}

// pingServer verifies the device is still registered by issuing a heartbeat.
// Returns an error if the server rejects the credentials.
func pingServer(cfg *AgentConfig, creds *AgentCredentials) error {
	resp, err := apiDo(cfg, creds, "POST", "/agents/heartbeat", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

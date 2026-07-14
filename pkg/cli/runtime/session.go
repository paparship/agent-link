package rt

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/team/agentlink/pkg/adapter"
	api "github.com/team/agentlink/pkg/cli/net"
)

// ErrNeedsAgentType is returned by RunSessionAdd when no agent type was given.
// The caller should exit non-zero without printing it again — RunSessionAdd
// has already emitted the NEEDS_AGENT_TYPE guidance the agent must act on.
var ErrNeedsAgentType = errors.New("agent type required")

func RunSessionAdd(name, agentType string) error {
	cfg, creds, err := api.LoadAuth()
	if err != nil {
		return err
	}

	if err := checkTmux(); err != nil {
		return err
	}

	// Agent type is chosen at creation and is permanent (issue 35). Without an
	// explicit --type we do not guess: emit guidance telling the agent to
	// confirm with the user and re-run, and create nothing.
	if agentType == "" {
		printNeedsAgentType(name)
		return ErrNeedsAgentType
	}
	launcher := adapter.NewLauncher(agentType)
	if launcher == nil {
		return fmt.Errorf("unknown agent type %q; supported: %s", agentType, strings.Join(adapter.SupportedAgents(), ", "))
	}
	if err := launcher.CheckPrereqs(); err != nil {
		return fmt.Errorf("agent type %q not usable: %w", agentType, err)
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

	// Write .agentlink.toml (records the permanent agent type)
	tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
	if err := api.WriteSessionTOML(tomlPath, name, cfg.Device, agentType); err != nil {
		return fmt.Errorf("cannot write %s: %w", tomlPath, err)
	}

	// Write CLAUDE.md
	claudePath := filepath.Join(sessionDir, "CLAUDE.md")
	claudeContent := launcher.InitTemplate(name, cfg.Device)
	if err := os.WriteFile(claudePath, []byte(claudeContent), 0600); err != nil {
		return fmt.Errorf("cannot write %s: %w", claudePath, err)
	}

	// Launch tmux session and record Claude session_id
	sessions, err := launchSessions(cfg.BaseDir, agentType, launchOpts{
		Resume: false,
		NoPoll: !cfg.Poll.Enabled,
		Only:   name,
	})
	if err != nil {
		return fmt.Errorf("cannot launch session: %w", err)
	}

	// Record the session in config unconditionally. The name must be tracked
	// even when the id is empty (e.g. claude failed to start), otherwise
	// restart would not know this session exists (issue 30). UpdateSessionID
	// appends the [sessions] entry if the segment is absent.
	configPath := filepath.Join(os.Getenv("HOME"), ".agentlink", "config.toml")
	if err := api.UpdateSessionID(configPath, name, sessions[name]); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record session in config: %v\n", err)
	}

	fmt.Printf("✓ session %q created — agent: %s (permanent)\n", name, agentType)
	fmt.Println("  Report this agent type to the user to confirm.")
	fmt.Printf("  Directory: %s\n", sessionDir)

	return nil
}

func RunSessionRemove(name string) error {
	cfg, creds, err := api.LoadAuth()
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
		return fmt.Errorf("cannot remove the last session; use 'agentlink uninstall' instead")
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

func RunUninstall() error {
	cfg, creds, err := api.LoadAuth()
	if err != nil {
		return err
	}

	// Kill tmux sessions (best-effort)
	killSessionSessions(cfg.BaseDir)

	// Deregister from server
	resp, err := api.APIDo(cfg, creds, "DELETE", "/agents/device", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Delete work directory
	if cfg.BaseDir != "" {
		if err := os.RemoveAll(cfg.BaseDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", cfg.BaseDir, err)
		}
	}

	// Clean up local .agentlink directory
	agentlinkDir := filepath.Join(os.Getenv("HOME"), ".agentlink")
	if err := os.RemoveAll(agentlinkDir); err != nil {
		return fmt.Errorf("warning: device unregistered from server but could not remove %s: %w", agentlinkDir, err)
	}

	fmt.Println("✓ Device unregistered and local files cleaned")
	fmt.Println("  To also remove agentlink from PATH, edit ~/.bashrc or ~/.zshrc")
	return nil
}

// killSessionSessions kills tmux sessions for each subdirectory in baseDir.
// Best-effort; errors are silently ignored.
func killSessionSessions(baseDir string) {
	if baseDir == "" {
		return
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		exec.Command("tmux", "kill-session", "-t", "="+name).Run()
		exec.Command("tmux", "kill-session", "-t", "="+name+"-poller").Run()
	}
}

func RunAttach(session string) error {
	cfg, err := api.LoadConfig()
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

	// Check if tmux session exists. Use "=" for an exact match: without it,
	// tmux falls back to prefix matching, so "-t main" would silently match
	// "main-poller" when the real "main" session is gone (see issue 32).
	hasSession := exec.Command("tmux", "has-session", "-t", "="+session).Run() == nil

	if hasSession {
		cmd := exec.Command("tmux", "attach", "-t", "="+session)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Use this session's own agent type (falling back to the device default).
	agent := api.ReadSessionAgent(sessionDir)
	if agent == "" {
		agent = cfg.Agent
	}
	launcher := adapter.NewLauncher(agent)
	if launcher == nil {
		return fmt.Errorf("unknown agent type %q for session %q", agent, session)
	}
	name, args := launcher.Command()
	// Launch through sh so we can inject root env (e.g. IS_SANDBOX=1 for Claude
	// Code under root); `exec` hands the pane tty straight to the agent so the
	// TUI is unaffected. envPrefix is empty when not running as root.
	inner := rootEnvPrefix(launcher) + "exec " + shellJoin(append([]string{name}, args...))
	cmdArgs := []string{"new-session", "-c", sessionDir, "sh", "-c", inner}
	cmd := exec.Command("tmux", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// printNeedsAgentType emits the guidance an agent sees when it runs
// `session add` without --type: the supported catalog (annotated with local
// availability, all from the registry) and what to do next. English, since
// every non-basic command is run by the agent (issue 35).
func printNeedsAgentType(name string) {
	supported := adapter.SupportedAgents()
	avail := map[string]bool{}
	for _, a := range adapter.AvailableAgents() {
		avail[a] = true
	}
	fmt.Printf("NEEDS_AGENT_TYPE: session %q needs an agent type (permanent, cannot be changed later).\n", name)
	fmt.Println("Supported on this machine:")
	for _, a := range supported {
		mark := "not found"
		if avail[a] {
			mark = "installed"
		}
		fmt.Printf("  - %-8s (%s)\n", a, mark)
	}
	fmt.Println("Next:")
	fmt.Printf("  - if the user already chose a CLI, re-run: agentlink session add --type <%s> %s\n",
		strings.Join(supported, "|"), name)
	fmt.Println("  - otherwise ask the user which one, then re-run with --type")
}

// -- helpers --

func checkTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required but not found in PATH")
	}
	return nil
}

func fetchSessions(cfg *api.AgentConfig, creds *api.AgentCredentials) ([]string, error) {
	resp, err := api.APIDo(cfg, creds, "GET", "/agents/list", nil)
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

func patchSessions(cfg *api.AgentConfig, creds *api.AgentCredentials, sessions []string) error {
	resp, err := api.APIDo(cfg, creds, "PATCH", "/agents/sessions", map[string][]string{
		"sessions": sessions,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

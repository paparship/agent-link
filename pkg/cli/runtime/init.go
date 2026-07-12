package rt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/team/agentlink/pkg/adapter"
	api "github.com/team/agentlink/pkg/cli/net"
)

type InitOptions struct {
	Server   string
	Password string
	Device   string
	Path     string
	Agent    string
	NoPoll   bool
	Force    bool
}

type registerRequest struct {
	Device           string   `json:"device"`
	Sessions         []string `json:"sessions"`
	RegisterPassword string   `json:"register_password"`
}

type registerResponse struct {
	APIKey       string   `json:"api_key"`
	Device       string   `json:"device"`
	Sessions     []string `json:"sessions"`
	RegisteredAt string   `json:"registered_at"`
}

func RunInit(opts *InitOptions) error {
	if opts.Agent == "" {
		opts.Agent = "claude"
	}

	// Resolve device name
	device := opts.Device
	if device == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("cannot determine hostname: %w", err)
		}
		device = hostname
	}

	// Pre-check prerequisites
	launcher := adapter.NewLauncher(opts.Agent)
	if launcher == nil {
		return fmt.Errorf("unknown agent type %q", opts.Agent)
	}
	if err := launcher.CheckPrereqs(); err != nil {
		return err
	}

	// Resolve and validate target directory
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return fmt.Errorf("cannot resolve path %q: %w", opts.Path, err)
	}

	if _, err := os.Stat(absPath); err == nil {
		if !opts.Force {
			return fmt.Errorf("directory %q already exists; use --force to override", absPath)
		}
	}

	// Create directories
	agentlinkDir := filepath.Join(os.Getenv("HOME"), ".agentlink")
	if err := os.MkdirAll(agentlinkDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", agentlinkDir, err)
	}

	teamDirs := []string{
		filepath.Join(absPath, "main"),
		filepath.Join(absPath, "worker"),
	}
	for _, d := range teamDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
	}

	// Write config.toml (initial; [sessions] added after tmux launch records ids)
	configPath := filepath.Join(agentlinkDir, "config.toml")
	if err := api.WriteConfigTOML(configPath, opts.Server, device, absPath, opts.Agent, opts.NoPoll, nil); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	// Call register API
	regResp, err := registerDevice(opts.Server, device, opts.Password)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Write credentials.json
	credPath := filepath.Join(agentlinkDir, "credentials.json")
	cred := map[string]string{
		"api_key":       regResp.APIKey,
		"registered_at": regResp.RegisteredAt,
	}
	credData, _ := json.MarshalIndent(cred, "", "  ")
	if err := os.WriteFile(credPath, credData, 0600); err != nil {
		return fmt.Errorf("cannot write credentials: %w", err)
	}

	// Write .agentlink.toml and CLAUDE.md for each session
	for _, session := range regResp.Sessions {
		sessionDir := filepath.Join(absPath, session)
		tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
		if err := api.WriteSessionTOML(tomlPath, session, device); err != nil {
			return fmt.Errorf("cannot write %s: %w", tomlPath, err)
		}
		claudePath := filepath.Join(sessionDir, "CLAUDE.md")
		if err := os.WriteFile(claudePath, []byte(launcher.InitTemplate(session, device)), 0600); err != nil {
			return fmt.Errorf("cannot write %s: %w", claudePath, err)
		}
	}

	// Launch tmux sessions and record Claude session_ids
	sessions, err := launchSessions(absPath, opts.Agent, launchOpts{
		Resume:   false,
		NoPoll:   opts.NoPoll,
		Existing: nil,
	})
	if err != nil {
		return fmt.Errorf("cannot launch sessions: %w", err)
	}

	// Rewrite config.toml with [sessions] segment
	if err := api.WriteConfigTOML(configPath, opts.Server, device, absPath, opts.Agent, opts.NoPoll, sessions); err != nil {
		return fmt.Errorf("cannot rewrite config with session ids: %w", err)
	}

	// Print success
	fmt.Printf("✓ Agent team initialized at %s\n", absPath)
	fmt.Printf("✓ Device %q registered (sessions: %s)\n", device, strings.Join(regResp.Sessions, ", "))
	fmt.Println("✓ tmux sessions created: main, worker")
	if opts.NoPoll {
		fmt.Println("  Auto-polling disabled (use agentlink poll to start manually)")
	} else {
		fmt.Println("✓ poller sessions created: main-poller, worker-poller")
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  agentlink attach worker    # switch to worker session")

	return nil
}

// launchOpts controls how launchSessions starts each tmux session.
type launchOpts struct {
	// Resume=true starts the agent with --resume <session_id>; false starts fresh.
	Resume bool
	// NoPoll suppresses the per-session poller tmux session.
	NoPoll bool
	// Existing maps session name → recorded session_id (from 23a).
	// On Resume, sessions with a non-empty id use --resume; others fall back
	// to --continue (23c). Ignored when Resume=false.
	Existing map[string]string
	// Only, when non-empty, launches just that single session (used by
	// `session add`). When empty, launches "main" and "worker".
	Only string
}

// launchSessions starts tmux session(s) + optional poller under baseDir.
// Returns a map of session name → Claude session_id (recorded from
// ~/.claude.json after each launch).
//
// Sessions are launched serially so that each Claude Code process writes a
// distinct lastSessionId before the next one starts; otherwise both would
// race on the same ~/.claude.json field.
func launchSessions(baseDir, agent string, opts launchOpts) (map[string]string, error) {
	selfExe, _ := os.Executable()
	launcher := adapter.NewLauncher(agent)
	if launcher == nil {
		return nil, fmt.Errorf("unknown agent type %q", agent)
	}

	var sessions []string
	if opts.Only != "" {
		sessions = []string{opts.Only}
	} else {
		sessions = []string{"main", "worker"}
	}
	recorded := map[string]string{}

	for _, session := range sessions {
		// Kill any pre-existing tmux session for this name
		exec.Command("tmux", "kill-session", "-t", session).Run()
		exec.Command("tmux", "kill-session", "-t", session+"-poller").Run()

		dir := filepath.Join(baseDir, session)
		name, args := launcher.Command()
		if opts.Resume {
			sessionID := opts.Existing[session]
			args = launcher.ResumeArgs(sessionID)
		}
		cmdArgs := append([]string{"new-session", "-d", "-s", session, "-c", dir, name}, args...)
		if err := exec.Command("tmux", cmdArgs...).Run(); err != nil {
			return nil, fmt.Errorf("cannot create tmux session %q: %w", session, err)
		}

		fmt.Printf("  %s — waiting for Claude to start", session)
		id := opts.Existing[session]
		if !opts.Resume || id == "" {
			recorded[session] = readClaudeSessionIDWithTimeout(10 * time.Second)
		} else {
			recorded[session] = id
		}
		if recorded[session] != "" {
			fmt.Println(" ✓")
		} else {
			fmt.Println(" (session_id unavailable, continue fallback)")
		}

		if !opts.NoPoll {
			exec.Command("tmux", "new-session", "-d", "-s", session+"-poller", "-c", dir, selfExe, "poll").Run()
		}
	}

	return recorded, nil
}

// readClaudeSessionIDWithTimeout polls ~/.claude.json for lastSessionId,
// waiting up to timeout for a non-empty value. Returns "" on timeout.
func readClaudeSessionIDWithTimeout(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		id, _ := readClaudeSessionID()
		if id != "" {
			return id
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

// readClaudeSessionID reads ~/.claude.json and returns lastSessionId.
// Returns "" if the file is absent or the field is empty.
func readClaudeSessionID() (string, error) {
	path := filepath.Join(os.Getenv("HOME"), ".claude.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		LastSessionID string `json:"lastSessionId"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	return doc.LastSessionID, nil
}

func registerDevice(server, device, password string) (*registerResponse, error) {
	body := registerRequest{
		Device:           device,
		Sessions:         []string{"main", "worker"},
		RegisterPassword: password,
	}
	data, _ := json.Marshal(body)

	url := server + "/agents/register"
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server %s: %w", server, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var regResp registerResponse
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return nil, fmt.Errorf("invalid server response: %w", err)
	}
	return &regResp, nil
}

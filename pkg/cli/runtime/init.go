package rt

import (
	"bytes"
	"crypto/rand"
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
	// Agent is the device-level default agent type (fallback for sessions
	// without a per-session type). Derived from SessionAgents["main"].
	Agent string
	// SessionAgents maps session name → agent type (issue 35). Populated by
	// RunInit (interactive prompt or auto-detect) when empty.
	SessionAgents map[string]string
	NoPoll        bool
	Force         bool
	// Interactive is set when init ran the terminal wizard. It lets RunInit
	// re-prompt for the password on a 401 instead of failing outright.
	Interactive bool
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
	// Resolve device name
	device := opts.Device
	if device == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("cannot determine hostname: %w", err)
		}
		device = hostname
	}

	// Resolve each session's agent type (per-session, immutable — issue 35).
	// Interactive init asks for main and worker separately; non-interactive
	// picks the sole installed agent (else the first supported).
	initSessions := []string{"main", "worker"}
	if opts.SessionAgents == nil {
		opts.SessionAgents = map[string]string{}
	}
	for _, session := range initSessions {
		if opts.SessionAgents[session] != "" {
			continue
		}
		a, err := resolveAgentFor(session, opts.Interactive)
		if err != nil {
			return err
		}
		opts.SessionAgents[session] = a
	}
	// Device-level default (fallback for legacy sessions without a type).
	opts.Agent = opts.SessionAgents["main"]

	// Pre-check prerequisites for every distinct agent we are about to launch.
	for _, a := range opts.SessionAgents {
		launcher := adapter.NewLauncher(a)
		if launcher == nil {
			return fmt.Errorf("unknown agent type %q", a)
		}
		if err := launcher.CheckPrereqs(); err != nil {
			return err
		}
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

	// Register first (network-only, no local side effects) so a failure —
	// wrong password or unreachable server — leaves nothing behind on disk.
	regResp, err := registerDeviceInteractive(opts, device)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
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

	// Write .agentlink.toml and CLAUDE.md for each session, each with its own
	// (immutable) agent type.
	for _, session := range regResp.Sessions {
		agent := opts.SessionAgents[session]
		if agent == "" {
			agent = opts.Agent
		}
		sessionDir := filepath.Join(absPath, session)
		tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
		if err := api.WriteSessionTOML(tomlPath, session, device, agent); err != nil {
			return fmt.Errorf("cannot write %s: %w", tomlPath, err)
		}
		claudePath := filepath.Join(sessionDir, "CLAUDE.md")
		if err := os.WriteFile(claudePath, []byte(adapter.NewLauncher(agent).InitTemplate(session, device)), 0600); err != nil {
			return fmt.Errorf("cannot write %s: %w", claudePath, err)
		}
	}

	// Launch tmux sessions and record Claude session_ids
	sessions, err := launchSessions(absPath, opts.Agent, launchOpts{
		Resume: false,
		NoPoll: opts.NoPoll,
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
	// Resume=true resumes the session's existing conversation via --continue;
	// false starts a brand-new conversation with a freshly generated session id.
	Resume bool
	// NoPoll suppresses the per-session poller tmux session.
	NoPoll bool
	// Only, when non-empty, launches just that single session (used by
	// `session add`). When empty, launches "main" and "worker".
	Only string
}

// launchSessions starts tmux session(s) + optional poller under baseDir.
// Returns a map of session name → Claude session_id.
//
// For a fresh launch (Resume=false) agentlink generates a UUID per session and
// passes it via --session-id, so the id is known up front — no reading back
// from ~/.claude.json (which Claude only writes on exit anyway; see issue 34).
// For Resume=true it uses --continue: each session owns its own working
// directory, so "most recent conversation in this dir" is unambiguous and
// survives an in-session /clear (which would orphan a stored --resume id).
//
// Each session's agent type is read from its own .agentlink.toml (written
// before launch); defaultAgent is the device-level fallback for sessions
// without a recorded type (see issue 35).
//
// Sessions are launched serially, mainly so their startup output stays legible.
func launchSessions(baseDir, defaultAgent string, opts launchOpts) (map[string]string, error) {
	selfExe, _ := os.Executable()

	var sessions []string
	if opts.Only != "" {
		sessions = []string{opts.Only}
	} else {
		sessions = []string{"main", "worker"}
	}
	recorded := map[string]string{}

	for _, session := range sessions {
		// Kill any pre-existing tmux session for this name. Use "=" for an
		// exact match so a stale "main-poller" is not prefix-matched by
		// "-t main" (see issue 32).
		exec.Command("tmux", "kill-session", "-t", "="+session).Run()
		exec.Command("tmux", "kill-session", "-t", "="+session+"-poller").Run()

		dir := filepath.Join(baseDir, session)

		// Resolve this session's agent (per-session, immutable). Fall back to
		// the device default for legacy sessions without a recorded type.
		agent := api.ReadSessionAgent(dir)
		if agent == "" {
			agent = defaultAgent
		}
		launcher := adapter.NewLauncher(agent)
		if launcher == nil {
			return nil, fmt.Errorf("unknown agent type %q for session %q", agent, session)
		}
		name, _ := launcher.Command()

		// Decide launch args + the session id we will record.
		var args []string
		var sid string
		if opts.Resume {
			args = launcher.ResumeArgs("") // --continue
		} else {
			var err error
			sid, err = newSessionID()
			if err != nil {
				return nil, fmt.Errorf("cannot generate session id: %w", err)
			}
			args = launcher.NewSessionArgs(sid)
		}

		// Launch the agent through a shell wrapper that captures stderr and the
		// exit code to a per-session log. Without this, a claude that exits at
		// startup (root refusing --dangerously-skip-permissions, --continue
		// failing, etc.) takes its error message down with the tmux session and
		// leaves no trace (see issue 32). stderr goes to the file; stdout stays
		// on the pane tty so the TUI is unaffected.
		logPath := sessionLogPath(session)
		wrapped := fmt.Sprintf("%s 2>>%s; echo \"[exited code=$? at $(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)]\" >>%s",
			shellJoin(append([]string{name}, args...)), shellQuote(logPath), shellQuote(logPath))
		cmdArgs := []string{"new-session", "-d", "-s", session, "-c", dir, "sh", "-c", wrapped}
		if err := exec.Command("tmux", cmdArgs...).Run(); err != nil {
			return nil, fmt.Errorf("cannot create tmux session %q: %w", session, err)
		}

		// Liveness probe: if the session is already gone, claude exited at
		// startup. Report the reason from the log instead of silently
		// continuing (which would leave only the poller behind).
		time.Sleep(1 * time.Second)
		if !tmuxSessionAlive(session) {
			fmt.Printf("  ✗ %s 启动失败,claude 已退出\n", session)
			if tail := tailFile(logPath, 12); tail != "" {
				fmt.Printf("%s\n", indent(tail, "    "))
			}
			fmt.Printf("    完整日志: %s\n", logPath)
			recorded[session] = ""
			continue
		}

		// Session is alive. For a fresh launch we already know the id (we chose
		// it); for resume we let --continue pick the conversation and leave the
		// recorded id empty (config's stored id is display-only after /clear).
		recorded[session] = sid
		if opts.Resume {
			fmt.Printf("  %s ✓ (--continue)\n", session)
		} else {
			fmt.Printf("  %s ✓ (session %s)\n", session, sid)
		}

		if !opts.NoPoll {
			exec.Command("tmux", "new-session", "-d", "-s", session+"-poller", "-c", dir, selfExe, "poll").Run()
		}
	}

	return recorded, nil
}

// tmuxSessionAlive reports whether a tmux session with the exact given name
// exists. The "=" prefix forces an exact match (no prefix fallback).
func tmuxSessionAlive(session string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+session).Run() == nil
}

// sessionLogPath returns ~/.agentlink/logs/<session>.log, creating the logs
// directory if needed.
func sessionLogPath(session string) string {
	logDir := filepath.Join(os.Getenv("HOME"), ".agentlink", "logs")
	os.MkdirAll(logDir, 0755)
	return filepath.Join(logDir, session+".log")
}

// tailFile returns the last n lines of a file, or "" if it cannot be read.
func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// indent prefixes every line of s with prefix.
func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// shellQuote single-quotes a string for safe use in a /bin/sh command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin quotes and space-joins args into a single /bin/sh command string.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// resolveAgentFor decides a session's agent type. Interactive callers prompt
// (annotated with local availability); non-interactive callers pick the sole
// installed agent, else the first supported. Errors when none are installed.
func resolveAgentFor(session string, interactive bool) (string, error) {
	supported := adapter.SupportedAgents()
	avail := adapter.AvailableAgents()
	if len(avail) == 0 {
		return "", fmt.Errorf("no supported agent found on this machine (need one of: %s in PATH)", strings.Join(supported, ", "))
	}
	if interactive {
		return promptAgentChoice(session, supported, avail), nil
	}
	if len(avail) == 1 {
		return avail[0], nil
	}
	return supported[0], nil
}

// newSessionID returns a random RFC 4122 version-4 UUID string, suitable for
// Claude Code's --session-id (which requires a valid UUID). Generated with
// crypto/rand to avoid pulling in a uuid dependency.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// registerDeviceInteractive registers the device. When running interactively
// and the server rejects the password (401), it re-prompts for the password
// and retries, up to two additional attempts, instead of aborting.
func registerDeviceInteractive(opts *InitOptions, device string) (*registerResponse, error) {
	for attempt := 0; ; attempt++ {
		regResp, status, err := registerDevice(opts.Server, device, opts.Password)
		if err == nil {
			return regResp, nil
		}
		if opts.Interactive && status == http.StatusUnauthorized && attempt < 2 {
			fmt.Printf("  %v\n", err)
			opts.Password = promptSecret("请重新输入注册密码")
			continue
		}
		return nil, err
	}
}

// registerDevice POSTs to /agents/register. It returns the HTTP status code
// alongside the error (0 on transport failure) so callers can distinguish a
// wrong password (401) from other failures.
func registerDevice(server, device, password string) (*registerResponse, int, error) {
	body := registerRequest{
		Device:           device,
		Sessions:         []string{"main", "worker"},
		RegisterPassword: password,
	}
	data, _ := json.Marshal(body)

	url := server + "/agents/register"
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("cannot connect to server %s: %w", server, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, resp.StatusCode, fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return nil, resp.StatusCode, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var regResp registerResponse
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("invalid server response: %w", err)
	}
	return &regResp, resp.StatusCode, nil
}

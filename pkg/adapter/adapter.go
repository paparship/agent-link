// Package adapter defines the abstraction layer between agentlink and
// the underlying CLI agent (e.g. Claude Code, OpenCode).
//
// agentlink's product direction is a generic network layer for all agents,
// not a Claude Code plugin. These interfaces decouple agent-specific
// launch and idle-detection logic so new agents can be plugged in without
// modifying agentlink core.
package adapter

// AgentLauncher handles agent process lifecycle.
type AgentLauncher interface {
	// Command returns the binary name and arguments used to start the agent.
	// The caller uses them with exec.Command(name, args...).
	Command() (name string, args []string)

	// ResumeArgs returns the arguments used to resume a prior session.
	// When sessionID is empty, returns args for a "continue last session"
	// fallback (used for configs created before session_id recording).
	ResumeArgs(sessionID string) []string

	// CheckPrereqs verifies the host environment has everything the agent needs
	// (e.g. the agent binary in PATH), returning an error if not.
	CheckPrereqs() error

	// InitTemplate returns the CLAUDE.md content for a given session and device.
	// agentlink writes this file into each session directory during init.
	InitTemplate(session string, device string) string

	// SessionIDPath returns the path to the JSON file where the agent records
	// its last session id (agentlink reads "lastSessionId" from it). Different
	// agents store it in different places, e.g. claude uses ~/.claude.json
	// while tclaude uses ~/.tclaude/.claude.json.
	SessionIDPath() string
}

// IdleDetector checks whether the agent's tmux pane is ready for input.
//
// agentlink injects messages only when the agent is both not busy AND
// has an empty prompt, i.e. !IsBusy(pane) && IsPromptEmpty(pane).
type IdleDetector interface {
	// IsBusy returns true when the agent is generating or running a tool.
	IsBusy(paneContent string) bool

	// IsPromptEmpty returns true when the agent's input box is empty
	// (bare prompt, nothing typed yet).
	IsPromptEmpty(paneContent string) bool
}

// NewLauncher returns an AgentLauncher for the named agent.
// Currently supported: "claude", "tclaude". Returns nil for unknown names.
func NewLauncher(agent string) AgentLauncher {
	switch agent {
	case "claude":
		return &ClaudeCodeLauncher{}
	case "tclaude":
		return &TclaudeLauncher{}
	}
	return nil
}

// NewDetector returns an IdleDetector for the named agent.
// Currently supported: "claude", "tclaude". Returns nil for unknown names.
func NewDetector(agent string) IdleDetector {
	switch agent {
	case "claude", "tclaude":
		// tclaude forwards to upstream Claude Code, so the TUI (and thus the
		// busy / prompt markers) is identical.
		return &ClaudeCodeDetector{}
	}
	return nil
}

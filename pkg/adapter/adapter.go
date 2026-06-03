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

	// CheckPrereqs verifies the host environment has everything the agent needs
	// (e.g. the agent binary in PATH), returning an error if not.
	CheckPrereqs() error

	// InitTemplate returns the CLAUDE.md content for a given session and device.
	// agentlink writes this file into each session directory during init.
	InitTemplate(session string, device string) string
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
// Currently supported: "claude". Returns nil for unknown names.
func NewLauncher(agent string) AgentLauncher {
	switch agent {
	case "claude":
		return &ClaudeCodeLauncher{}
	}
	return nil
}

// NewDetector returns an IdleDetector for the named agent.
// Currently supported: "claude". Returns nil for unknown names.
func NewDetector(agent string) IdleDetector {
	switch agent {
	case "claude":
		return &ClaudeCodeDetector{}
	}
	return nil
}

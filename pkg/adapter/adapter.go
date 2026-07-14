// Package adapter defines the abstraction layer between agentlink and
// the underlying CLI agent (e.g. Claude Code, OpenCode).
//
// agentlink's product direction is a generic network layer for all agents,
// not a Claude Code plugin. These interfaces decouple agent-specific
// launch and idle-detection logic so new agents can be plugged in without
// modifying agentlink core.
package adapter

import (
	"os/exec"
	"sort"
)

// AgentLauncher handles agent process lifecycle.
type AgentLauncher interface {
	// Command returns the binary name and arguments used to start the agent.
	// The caller uses them with exec.Command(name, args...).
	Command() (name string, args []string)

	// ResumeArgs returns the arguments used to resume a prior session.
	// When sessionID is empty, returns args for a "continue last session"
	// fallback (used for configs created before session_id recording).
	ResumeArgs(sessionID string) []string

	// NewSessionArgs returns the arguments to start a brand-new session bound
	// to a caller-chosen session id (a valid UUID). agentlink generates the id
	// itself and passes it in, so it knows the id up front without reading it
	// back from the agent's state files (see issue 34).
	NewSessionArgs(sessionID string) []string

	// CheckPrereqs verifies the host environment has everything the agent needs
	// (e.g. the agent binary in PATH), returning an error if not.
	CheckPrereqs() error

	// InitTemplate returns the CLAUDE.md content for a given session and device.
	// agentlink writes this file into each session directory during init.
	InitTemplate(session string, device string) string

	// RootEnv returns extra "KEY=VALUE" environment entries the agent needs to
	// run unattended as root. Applied by the launch code ONLY when the current
	// process is root (euid 0). Empty for agents that need nothing special.
	RootEnv() []string
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

// agentSpec describes how to construct the launcher and idle detector for one
// supported agent type.
type agentSpec struct {
	newLauncher func() AgentLauncher
	newDetector func() IdleDetector
}

// registry is the single source of truth for which agent types agentlink
// supports. Adding a new CLI is one entry here — NewLauncher/NewDetector,
// SupportedAgents (and thus the whoami hint, session-add validation and the
// missing-type guidance) all derive from it, so no prompt text is hardcoded
// per agent (see issue 35).
//
// tclaude forwards to upstream Claude Code, so it reuses ClaudeCodeDetector
// (identical TUI busy / prompt markers).
var registry = map[string]agentSpec{
	"claude": {
		newLauncher: func() AgentLauncher { return &ClaudeCodeLauncher{} },
		newDetector: func() IdleDetector { return &ClaudeCodeDetector{} },
	},
	"tclaude": {
		newLauncher: func() AgentLauncher { return &TclaudeLauncher{} },
		newDetector: func() IdleDetector { return &ClaudeCodeDetector{} },
	},
}

// NewLauncher returns an AgentLauncher for the named agent, or nil if unknown.
func NewLauncher(agent string) AgentLauncher {
	if spec, ok := registry[agent]; ok {
		return spec.newLauncher()
	}
	return nil
}

// NewDetector returns an IdleDetector for the named agent, or nil if unknown.
func NewDetector(agent string) IdleDetector {
	if spec, ok := registry[agent]; ok {
		return spec.newDetector()
	}
	return nil
}

// SupportedAgents returns the agent type names agentlink can launch, sorted.
// This is the catalog shown to the agent (whoami hint, missing-type guidance).
func SupportedAgents() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AvailableAgents returns the subset of SupportedAgents whose binary is found
// on PATH. Used only to annotate / order choices — never to remove options, so
// a wrapper or alias that LookPath misses cannot lock the user out.
func AvailableAgents() []string {
	var out []string
	for _, name := range SupportedAgents() {
		bin, _ := NewLauncher(name).Command()
		if _, err := exec.LookPath(bin); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// IsAvailable reports whether the named agent's binary is on PATH.
func IsAvailable(agent string) bool {
	l := NewLauncher(agent)
	if l == nil {
		return false
	}
	bin, _ := l.Command()
	_, err := exec.LookPath(bin)
	return err == nil
}

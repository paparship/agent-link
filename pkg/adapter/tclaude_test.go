package adapter

import (
	"strings"
	"testing"
)

func TestTclaudeLauncher(t *testing.T) {
	l := NewLauncher("tclaude")
	if l == nil {
		t.Fatal("NewLauncher(\"tclaude\") returned nil")
	}

	name, args := l.Command()
	if name != "tclaude" {
		t.Errorf("Command name: got %q, want \"tclaude\"", name)
	}
	if len(args) == 0 || args[0] != "--dangerously-skip-permissions" {
		t.Errorf("Command args: got %v", args)
	}

	if p := l.SessionIDPath(); !strings.HasSuffix(p, "/.tclaude/.claude.json") {
		t.Errorf("SessionIDPath: got %q, want suffix /.tclaude/.claude.json", p)
	}

	// ResumeArgs / InitTemplate are inherited from ClaudeCodeLauncher.
	if got := l.ResumeArgs(""); len(got) == 0 || got[0] != "--continue" {
		t.Errorf("ResumeArgs(\"\"): got %v, want --continue fallback", got)
	}
	if got := l.ResumeArgs("sid-1"); len(got) < 2 || got[0] != "--resume" || got[1] != "sid-1" {
		t.Errorf("ResumeArgs(\"sid-1\"): got %v", got)
	}

	if NewDetector("tclaude") == nil {
		t.Error("NewDetector(\"tclaude\") returned nil")
	}
}

func TestClaudeSessionIDPath(t *testing.T) {
	l := NewLauncher("claude")
	if p := l.SessionIDPath(); !strings.HasSuffix(p, "/.claude.json") {
		t.Errorf("claude SessionIDPath: got %q, want suffix /.claude.json", p)
	}
}

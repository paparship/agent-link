package adapter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// TclaudeLauncher launches Tencent's internal Claude Code wrapper (tclaude).
// tclaude forwards all agent args to upstream Claude Code as-is and points it
// at ~/.tclaude via CLAUDE_CONFIG_DIR, so it differs from claude only in the
// binary name, the prereq check, and where lastSessionId is recorded. It
// embeds ClaudeCodeLauncher to reuse ResumeArgs and InitTemplate.
type TclaudeLauncher struct {
	ClaudeCodeLauncher
}

func (l *TclaudeLauncher) Command() (name string, args []string) {
	return "tclaude", []string{"--dangerously-skip-permissions"}
}

func (l *TclaudeLauncher) CheckPrereqs() error {
	for _, cmd := range []string{"tmux", "tclaude"} {
		if _, err := exec.LookPath(cmd); err != nil {
			return fmt.Errorf("require %s to be installed", cmd)
		}
	}
	return nil
}

// SessionIDPath returns ~/.tclaude/.claude.json. tclaude sets
// CLAUDE_CONFIG_DIR=~/.tclaude, so the underlying Claude Code records
// lastSessionId there rather than in ~/.claude.json.
func (l *TclaudeLauncher) SessionIDPath() string {
	return filepath.Join(os.Getenv("HOME"), ".tclaude", ".claude.json")
}

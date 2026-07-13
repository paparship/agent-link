package adapter

import (
	"fmt"
	"os/exec"
	"strings"
)

// TclaudeLauncher implements AgentLauncher for tclaude, Tencent's internal
// Claude Code wrapper. tclaude forwards all non-self arguments to the upstream
// Claude Code binary and sets CLAUDE_CONFIG_DIR=~/.tclaude, so its launch and
// resume behaviour is identical to claude apart from the binary name.
//
// It embeds ClaudeCodeLauncher to inherit ResumeArgs and InitTemplate; only
// the binary name and prerequisite check differ.
type TclaudeLauncher struct {
	ClaudeCodeLauncher
}

func (l *TclaudeLauncher) Command() (name string, args []string) {
	return "tclaude", []string{"--dangerously-skip-permissions"}
}

func (l *TclaudeLauncher) CheckPrereqs() error {
	var missing []string
	for _, cmd := range []string{"tmux", "tclaude"} {
		if _, err := exec.LookPath(cmd); err != nil {
			missing = append(missing, cmd)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("require %s to be installed", strings.Join(missing, " and "))
	}
	return nil
}

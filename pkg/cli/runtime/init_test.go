package rt

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/team/agentlink/pkg/adapter"
	api "github.com/team/agentlink/pkg/cli/net"
)

func TestCheckPrereqs(t *testing.T) {
	l := adapter.NewLauncher("claude")

	t.Run("both available in PATH", func(t *testing.T) {
		err := l.CheckPrereqs()
		if err != nil {
			t.Logf("prereqs check: %v (may be expected in some environments)", err)
		}
	})

	t.Run("empty PATH", func(t *testing.T) {
		t.Setenv("PATH", "")
		err := l.CheckPrereqs()
		if err == nil {
			t.Fatal("expected error with empty PATH")
		}
		if !strings.Contains(err.Error(), "to be installed") {
			t.Errorf("error should mention to be installed, got: %s", err)
		}
	})

	t.Run("partial PATH with tmux only", func(t *testing.T) {
		// Find where tmux is and only expose that directory
		tmuxPath, err := exec.LookPath("tmux")
		if err != nil {
			t.Skip("tmux not found in PATH, can't test")
		}
		tmuxDir := filepath.Dir(tmuxPath)
		t.Setenv("PATH", tmuxDir)
		err = l.CheckPrereqs()
		if err == nil {
			t.Fatal("expected error when claude is missing")
		}
		if !strings.Contains(err.Error(), "claude") {
			t.Errorf("error should mention claude, got: %s", err)
		}
		if strings.Contains(err.Error(), "tmux") {
			t.Errorf("error should not mention tmux when it's available, got: %s", err)
		}
	})
}

func TestWriteConfigTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	err := api.WriteConfigTOML(path, "http://server:8080", "my-device", "/tmp/agent_team", "claude", false, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, `server = "http://server:8080"`) {
		t.Errorf("missing server, got: %s", content)
	}
	if !strings.Contains(content, `device = "my-device"`) {
		t.Errorf("missing device, got: %s", content)
	}
	if !strings.Contains(content, `base_dir = "/tmp/agent_team"`) {
		t.Errorf("missing base_dir, got: %s", content)
	}
	if !strings.Contains(content, `agent = "claude"`) {
		t.Errorf("missing agent, got: %s", content)
	}
}

func TestWriteSessionTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agentlink.toml")

	err := api.WriteSessionTOML(path, "worker", "my-device", "claude")
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, `session = "worker"`) {
		t.Errorf("missing session, got: %s", content)
	}
	if !strings.Contains(content, `device = "my-device"`) {
		t.Errorf("missing device, got: %s", content)
	}
	if !strings.Contains(content, `agent = "claude"`) {
		t.Errorf("missing agent, got: %s", content)
	}
}

func TestWriteSessionTOMLFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agentlink.toml")

	api.WriteSessionTOML(path, "main", "dev", "claude")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %o", info.Mode().Perm())
	}
}


func TestRunInitE2E_existingDir(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH, skipping")
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	workDir := filepath.Join(homeDir, "already-there")
	os.MkdirAll(workDir, 0755)

	opts := &InitOptions{
		Server:   "http://localhost:1",
		Password: "test-pw",
		Device:   "test-device",
		Path:     workDir,
	}

	err := RunInit(opts)
	if err == nil {
		t.Fatal("expected error for existing directory")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %s", err)
	}
}
func TestPollConfigParsing(t *testing.T) {
	t.Run("poll enabled true", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		config := `server = "http://srv:8080"
device = "dev"
base_dir = "/tmp/agent_team"
agent = "claude"

[poll]
enabled = true
interval = 10
`
		os.WriteFile(filepath.Join(agentlinkDir, "config.toml"), []byte(config), 0600)

		cfg, err := api.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Poll.Enabled {
			t.Error("expected Poll.Enabled=true")
		}
		if cfg.Poll.Interval != 10 {
			t.Errorf("expected Poll.Interval=10, got %d", cfg.Poll.Interval)
		}
	})

	t.Run("poll enabled false", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		config := `server = "http://srv:8080"
device = "dev"
base_dir = "/tmp/agent_team"
agent = "claude"

[poll]
enabled = false
interval = 5
`
		os.WriteFile(filepath.Join(agentlinkDir, "config.toml"), []byte(config), 0600)

		cfg, err := api.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Poll.Enabled {
			t.Error("expected Poll.Enabled=false")
		}
	})

	t.Run("poll section missing defaults to enabled", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), "http://srv:8080", "dev", "/tmp/agent_team", "claude", false, nil)

		cfg, err := api.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Poll.Enabled {
			t.Error("expected Poll.Enabled=true by default")
		}
		if cfg.Poll.Interval != 5 {
			t.Errorf("expected Poll.Interval=5 (default), got %d", cfg.Poll.Interval)
		}
	})

	t.Run("poll interval from config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		config := `server = "http://srv:8080"
device = "dev"
base_dir = "/tmp/agent_team"
agent = "claude"

[poll]
enabled = true
interval = 30
`
		os.WriteFile(filepath.Join(agentlinkDir, "config.toml"), []byte(config), 0600)

		cfg, err := api.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Poll.Interval != 30 {
			t.Errorf("expected Poll.Interval=30, got %d", cfg.Poll.Interval)
		}
	})
}

func TestRunPoll_disabled(t *testing.T) {
	t.Run("poll disabled exits clean", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		config := `server = "http://srv:8080"
device = "dev"
base_dir = "` + filepath.Join(homeDir, "agent_team") + `"
agent = "claude"

[poll]
enabled = false
`
		os.WriteFile(filepath.Join(agentlinkDir, "config.toml"), []byte(config), 0600)

		creds := map[string]string{"api_key": "sk_live_test"}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		sessionDir := filepath.Join(homeDir, "agent_team", "worker")
		os.MkdirAll(sessionDir, 0755)
		api.WriteSessionTOML(filepath.Join(sessionDir, ".agentlink.toml"), "worker", "dev", "claude")

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunPoll()
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestNewSessionID(t *testing.T) {
	// UUID v4 canonical form: 8-4-4-4-12 hex, version nibble 4, variant 8/9/a/b.
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := newSessionID()
		if err != nil {
			t.Fatalf("newSessionID() error: %v", err)
		}
		if !re.MatchString(id) {
			t.Fatalf("newSessionID() = %q, not a valid v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("newSessionID() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

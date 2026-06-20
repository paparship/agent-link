package rt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/team/agentlink/pkg/cli/net"
)

// mockIdleDetector implements adapter.IdleDetector for testing.
type mockIdleDetector struct {
	busy        bool
	promptEmpty bool
}

func (m *mockIdleDetector) IsBusy(_ string) bool        { return m.busy }
func (m *mockIdleDetector) IsPromptEmpty(_ string) bool { return m.promptEmpty }

// ---------------------------------------------------------------------------
// Layer 2 — Poller logic with mocked deps
// ---------------------------------------------------------------------------

func TestPoller_injectsWhenIdle(t *testing.T) {
	msgContent := "task dispatch: write tests"

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{
				{ID: "m1", Type: "msg", Content: msgContent, FromDevice: "dev-a", FromSession: "main"},
			},
		})
	}))
	defer mockSrv.Close()

	var injected string
	captureCalls := 0
	ctx, cancel := context.WithCancel(context.Background())

	p := &Poller{
		Session:      "worker",
		Server:       mockSrv.URL,
		APIKey:       "sk_test",
		Interval:     10 * time.Millisecond,
		Ctx:          ctx,
		Stdout:       io.Discard,
		IdleDetector: &mockIdleDetector{busy: false, promptEmpty: true},
		capturePane: func(string) (string, error) {
			captureCalls++
			if captureCalls >= 3 {
				cancel()
			}
			return "❯\n", nil // idle
		},
		sendKeys: func(_ string, text string) error {
			injected = text
			return nil
		},
		httpDo: http.DefaultClient.Do,
	}

	p.Run()
	expected := "[来自 dev-a:main 的消息] " + msgContent
	if injected != expected {
		t.Errorf("expected injected=%q, got %q", expected, injected)
	}
}

func TestPoller_skipsWhenBusy(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{
				{ID: "m1", Type: "msg", Content: "should not inject", FromDevice: "dev-a", FromSession: "main"},
			},
		})
	}))
	defer mockSrv.Close()

	injectCalls := 0
	ctx, cancel := context.WithCancel(context.Background())

	p := &Poller{
		Session:      "worker",
		Server:       mockSrv.URL,
		APIKey:       "sk_test",
		Interval:     10 * time.Millisecond,
		Ctx:          ctx,
		Stdout:       io.Discard,
		IdleDetector: &mockIdleDetector{busy: true},
		capturePane: func(string) (string, error) {
			return "❯", nil
		},
		sendKeys: func(_ string, text string) error {
			injectCalls++
			return nil
		},
		httpDo: http.DefaultClient.Do,
	}

	// Run for a few iterations then stop
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	p.Run()

	if injectCalls > 0 {
		t.Errorf("expected 0 injects when busy, got %d", injectCalls)
	}
}

func TestPoller_skipsWhenCapturerFails(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{
				{ID: "m1", Type: "msg", Content: "msg", FromDevice: "dev-a", FromSession: "main"},
			},
		})
	}))
	defer mockSrv.Close()

	injectCalls := 0
	ctx, cancel := context.WithCancel(context.Background())

	p := &Poller{
		Session:      "worker",
		Server:       mockSrv.URL,
		APIKey:       "sk_test",
		Interval:     10 * time.Millisecond,
		Ctx:          ctx,
		Stdout:       io.Discard,
		IdleDetector: &mockIdleDetector{busy: false, promptEmpty: true},
		capturePane: func(string) (string, error) {
			return "", io.ErrUnexpectedEOF // capture failed
		},
		sendKeys: func(_ string, text string) error {
			injectCalls++
			return nil
		},
		httpDo: http.DefaultClient.Do,
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	p.Run()

	if injectCalls > 0 {
		t.Errorf("expected 0 injects when capture fails, got %d", injectCalls)
	}
}

func TestPoller_skipsWhenInboxEmpty(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{Items: []pollerInboxItem{}})
	}))
	defer mockSrv.Close()

	injectCalls := 0
	ctx, cancel := context.WithCancel(context.Background())

	p := &Poller{
		Session:      "worker",
		Server:       mockSrv.URL,
		APIKey:       "sk_test",
		Interval:     10 * time.Millisecond,
		Ctx:          ctx,
		Stdout:       io.Discard,
		IdleDetector: &mockIdleDetector{busy: false, promptEmpty: true},
		capturePane: func(string) (string, error) {
			return "❯\n", nil
		},
		sendKeys: func(_ string, text string) error {
			injectCalls++
			return nil
		},
		httpDo: http.DefaultClient.Do,
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	p.Run()

	if injectCalls > 0 {
		t.Errorf("expected 0 injects when inbox empty, got %d", injectCalls)
	}
}

// ---------------------------------------------------------------------------
// Layer 3 — RunPoll entry point (error paths only)
// ---------------------------------------------------------------------------

func TestRunPoll_errors(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunPoll()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		os.MkdirAll(filepath.Join(homeDir, ".agentlink"), 0755)
		api.WriteConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir, "claude", false, nil)

		err := RunPoll()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "credentials file not found") {
			t.Errorf("expected credentials error, got: %s", err)
		}
	})

	t.Run("missing session file", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		os.MkdirAll(filepath.Join(homeDir, ".agentlink"), 0755)
		api.WriteConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_test"}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(homeDir, ".agentlink", "credentials.json"), credData, 0600)

		err := RunPoll()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ".agentlink.toml not found") {
			t.Errorf("expected .agentlink.toml error, got: %s", err)
		}
	})
}
func TestRunPoll_disabledByConfig(t *testing.T) {
	t.Run("poll disabled returns nil", func(t *testing.T) {
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
`
		os.WriteFile(filepath.Join(agentlinkDir, "config.toml"), []byte(config), 0600)

		creds := map[string]string{"api_key": "sk_live_test"}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunPoll()
		if err != nil {
			t.Fatal(err)
		}
	})
}

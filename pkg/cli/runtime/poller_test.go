package rt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestPoller_injectsTaskWithGuidance(t *testing.T) {
	taskContent := "查 prod 为什么 500"
	taskID := "fix-001"

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{
				{ID: "t1", Type: "task", TaskID: taskID, Content: taskContent, FromDevice: "dev-a", FromSession: "main"},
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
			return "❯\n", nil
		},
		sendKeys: func(_ string, text string) error {
			injected = text
			return nil
		},
		httpDo: http.DefaultClient.Do,
	}

	p.Run()

	// Check prefix
	if !strings.Contains(injected, "[来自 dev-a:main 的任务 fix-001]") {
		t.Errorf("injected missing task prefix, got: %s", injected)
	}
	// Check content preserved
	if !strings.Contains(injected, taskContent) {
		t.Errorf("injected missing task content, got: %s", injected)
	}
	// Check completed guidance
	if !strings.Contains(injected, "agentlink task result fix-001 completed") {
		t.Errorf("injected missing completed guidance, got: %s", injected)
	}
	// Check suspended guidance
	if !strings.Contains(injected, "agentlink task result fix-001 suspended") {
		t.Errorf("injected missing suspended guidance, got: %s", injected)
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

// TestPoller_acksAfterInjectAndDedups: a successful inject is acked, and a
// message redelivered because its ack "was lost" is re-acked, not re-injected
// (issue 37 dedup via lastInjectedID). The mock always returns the same msg.
func TestPoller_acksAfterInjectAndDedups(t *testing.T) {
	var mu sync.Mutex
	var acked []string
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/inbox/ack" {
			var body struct{ Session, ID string }
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			acked = append(acked, body.ID)
			mu.Unlock()
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{{ID: "m1", Type: "msg", Content: "hi", FromDevice: "dev-a", FromSession: "main"}},
		})
	}))
	defer mockSrv.Close()

	injectCount := 0
	captureCalls := 0
	ctx, cancel := context.WithCancel(context.Background())
	p := &Poller{
		Session: "worker", Server: mockSrv.URL, APIKey: "sk_test",
		Interval: 5 * time.Millisecond, Ctx: ctx, Stdout: io.Discard,
		IdleDetector: &mockIdleDetector{busy: false, promptEmpty: true},
		capturePane: func(string) (string, error) {
			captureCalls++
			if captureCalls >= 5 {
				cancel()
			}
			return "❯\n", nil
		},
		sendKeys: func(_ string, _ string) error { injectCount++; return nil },
		httpDo:   http.DefaultClient.Do,
	}
	p.Run()

	if injectCount != 1 {
		t.Errorf("expected exactly 1 inject (dedup should suppress the rest), got %d", injectCount)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(acked) == 0 || acked[0] != "m1" {
		t.Errorf("expected message m1 to be acked, got %v", acked)
	}
}

// TestPoller_noAckOnSendKeysFailure: when inject fails, the message must NOT be
// acked (so the server redelivers it), and the poller keeps retrying (issue 37).
func TestPoller_noAckOnSendKeysFailure(t *testing.T) {
	var mu sync.Mutex
	ackCount := 0
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/inbox/ack" {
			mu.Lock()
			ackCount++
			mu.Unlock()
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pollerPullResponse{
			Items: []pollerInboxItem{{ID: "m1", Type: "msg", Content: "hi", FromDevice: "dev-a", FromSession: "main"}},
		})
	}))
	defer mockSrv.Close()

	injectAttempts := 0
	captureCalls := 0
	ctx, cancel := context.WithCancel(context.Background())
	p := &Poller{
		Session: "worker", Server: mockSrv.URL, APIKey: "sk_test",
		Interval: 5 * time.Millisecond, Ctx: ctx, Stdout: io.Discard,
		IdleDetector: &mockIdleDetector{busy: false, promptEmpty: true},
		capturePane: func(string) (string, error) {
			captureCalls++
			if captureCalls >= 4 {
				cancel()
			}
			return "❯\n", nil
		},
		sendKeys: func(_ string, _ string) error { injectAttempts++; return fmt.Errorf("boom") },
		httpDo:   http.DefaultClient.Do,
	}
	p.Run()

	mu.Lock()
	defer mu.Unlock()
	if ackCount != 0 {
		t.Errorf("must not ack when inject fails, got %d acks", ackCount)
	}
	if injectAttempts < 2 {
		t.Errorf("expected retries on failure (>=2 attempts), got %d", injectAttempts)
	}
}

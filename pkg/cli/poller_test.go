package cli

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
)

// ---------------------------------------------------------------------------
// Layer 1 — pure function: IsClaudeIdle
//
// Test data is based on real tmux capture-pane output observed from
// Claude Code v2.1.119 running with --dangerously-skip-permissions.
//
// Idle (bare prompt):
//   ────────────────────────────────────────────────────────────────────────────────
//   ❯
//   ────────────────────────────────────────────────────────────────────────────────
//     ⏵⏵ bypass permissions on (shift+tab to cycle)                ◈ max · /effort
//
// Typing (text after prompt):
//   ────────────────────────────────────────────────────────────────────────────────
//   ❯ some typed message
//   ────────────────────────────────────────────────────────────────────────────────
//     ⏵⏵ bypass permissions on (shift+tab to cycle)                ◈ max · /effort
//
// Busy (generating / running a tool):
//   ────────────────────────────────────────────────────────────────────────────────
//   ❯
//   ────────────────────────────────────────────────────────────────────────────────
//     ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks
// ---------------------------------------------------------------------------

func TestIsClaudeIdle(t *testing.T) {
	sep := "────────────────────────────────────────────────────────────────────────────────"
	statusIdle := "  ⏵⏵ bypass permissions on (shift+tab to cycle)                ◈ max · /effort"
	statusBusy := "  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks"

	tests := []struct {
		name string
		pane string
		want bool
	}{
		{
			name: "empty pane",
			pane: "",
			want: false, // no ❯ found → not ready
		},
		{
			name: "only newlines",
			pane: "\n\n\n\n",
			want: false,
		},
		{
			name: "busy — esc to interrupt in status bar",
			pane: sep + "\n❯\n" + sep + "\n" + statusBusy + "\n",
			want: false,
		},
		{
			name: "busy — esc to interrupt anywhere",
			pane: "some output\nesc to interrupt\nmore output\n",
			want: false,
		},
		{
			name: "idle — bare prompt",
			pane: sep + "\n❯\n" + sep + "\n" + statusIdle + "\n",
			want: true,
		},
		{
			name: "idle — prompt with trailing whitespace",
			pane: sep + "\n❯ \n" + sep + "\n" + statusIdle + "\n",
			want: true,
		},
		{
			name: "typing — text after prompt",
			pane: sep + "\n❯ some typed message\n" + sep + "\n" + statusIdle + "\n",
			want: false,
		},
		{
			name: "typing — short command",
			pane: sep + "\n❯ agentlink pull\n" + sep + "\n" + statusIdle + "\n",
			want: false,
		},
		{
			name: "claude output — no prompt visible",
			pane: "Here is some code:\nfunc foo() {\n  return 1\n}\n",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClaudeIdle(tt.pane)
			if got != tt.want {
				t.Errorf("IsClaudeIdle() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
		Session:  "worker",
		Server:   mockSrv.URL,
		APIKey:   "sk_test",
		Interval: 10 * time.Millisecond,
		Ctx:      ctx,
		Stdout:   io.Discard,
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
	if injected != msgContent {
		t.Errorf("expected injected=%q, got %q", msgContent, injected)
	}
}

func TestPoller_skipsWhenBusy(t *testing.T) {
	busyStatus := "  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt · ctrl+t to hide tasks"

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
		Session:  "worker",
		Server:   mockSrv.URL,
		APIKey:   "sk_test",
		Interval: 10 * time.Millisecond,
		Ctx:      ctx,
		Stdout:   io.Discard,
		capturePane: func(string) (string, error) {
			// Always busy — status bar has "esc to interrupt"
			return busyStatus + "\n", nil
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
		Session:  "worker",
		Server:   mockSrv.URL,
		APIKey:   "sk_test",
		Interval: 10 * time.Millisecond,
		Ctx:      ctx,
		Stdout:   io.Discard,
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
		Session:  "worker",
		Server:   mockSrv.URL,
		APIKey:   "sk_test",
		Interval: 10 * time.Millisecond,
		Ctx:      ctx,
		Stdout:   io.Discard,
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
		writeConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir)

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
		writeConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir)
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


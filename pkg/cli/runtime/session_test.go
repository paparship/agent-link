package rt

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/team/agentlink/pkg/cli/net"
)

func TestRunSessionAdd(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not in PATH, skipping")
	}
	// Mock server that handles both GET /agents/list and PATCH /agents/sessions
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agents/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{
					{"device": "test-device", "sessions": []string{"main", "worker"}},
				},
			})
		case "/agents/sessions":
			var req map[string][]string
			json.NewDecoder(r.Body).Decode(&req)
			r.Body.Close()
			sessions := req["sessions"]
			if len(sessions) != 3 || sessions[2] != "reviewer" {
				t.Errorf("expected sessions=[main,worker,reviewer], got %v", sessions)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer mockSrv.Close()

	t.Run("add session success", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		if err := RunSessionAdd("reviewer", "claude"); err != nil {
			t.Fatal(err)
		}

		// Verify directory created
		sessionDir := filepath.Join(homeDir, "reviewer")
		if _, err := os.Stat(sessionDir); err != nil {
			t.Errorf("expected session directory to exist: %s", sessionDir)
		}

		// Verify .agentlink.toml created
		tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
		if _, err := os.Stat(tomlPath); err != nil {
			t.Errorf("expected .agentlink.toml to exist: %s", tomlPath)
		}

		// Verify CLAUDE.md created
		claudePath := filepath.Join(sessionDir, "CLAUDE.md")
		if _, err := os.Stat(claudePath); err != nil {
			t.Errorf("expected CLAUDE.md to exist: %s", claudePath)
		}
	})

	t.Run("add duplicate session", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunSessionAdd("main", "claude")
		if err == nil {
			t.Fatal("expected error for duplicate session")
		}
		if !strings.Contains(err.Error(), "already registered") {
			t.Errorf("expected 'already registered' error, got: %s", err)
		}
	})

	t.Run("add session missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunSessionAdd("reviewer", "claude")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})

	t.Run("missing agent type returns guidance sentinel", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunSessionAdd("coder", "")
		if !errors.Is(err, ErrNeedsAgentType) {
			t.Fatalf("expected ErrNeedsAgentType, got %v", err)
		}
		// nothing should have been created
		if _, err := os.Stat(filepath.Join(homeDir, "coder")); err == nil {
			t.Error("session directory should not exist when type is missing")
		}
	})

	t.Run("unknown agent type is rejected", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunSessionAdd("coder", "bogus")
		if err == nil || !strings.Contains(err.Error(), "unknown agent type") {
			t.Fatalf("expected unknown agent type error, got %v", err)
		}
	})
}

func TestRunSessionRemove(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agents/list":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{
					{"device": "test-device", "sessions": []string{"main", "worker", "reviewer"}},
				},
			})
		case "/agents/sessions":
			var req map[string][]string
			json.NewDecoder(r.Body).Decode(&req)
			r.Body.Close()
			sessions := req["sessions"]
			if len(sessions) != 2 {
				t.Errorf("expected 2 sessions after removal, got %v", sessions)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
		}
	}))
	defer mockSrv.Close()

	t.Run("remove session success", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		// Create session directory so removal can clean it up
		os.MkdirAll(filepath.Join(homeDir, "reviewer"), 0755)

		if err := RunSessionRemove("reviewer"); err != nil {
			t.Fatal(err)
		}

		// Verify directory removed
		if _, err := os.Stat(filepath.Join(homeDir, "reviewer")); err == nil {
			t.Error("expected session directory to be removed")
		}
	})

	t.Run("remove non-existing session", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunSessionRemove("nonexistent")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %s", err)
		}
	})
}

func TestRunUninstall(t *testing.T) {
	t.Run("uninstall success", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" || r.URL.Path != "/agents/device" {
				t.Errorf("expected DELETE /agents/device, got %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		}))
		defer mockSrv.Close()

		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		if err := RunUninstall(); err != nil {
			t.Fatal(err)
		}

		// Verify local cleanup
		if _, err := os.Stat(agentlinkDir); err == nil {
			t.Error("expected .agentlink directory to be removed")
		}
	})

	t.Run("uninstall server error", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "server error"})
		}))
		defer mockSrv.Close()

		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), mockSrv.URL, "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunUninstall()
		if err == nil {
			t.Fatal("expected error for purge with server error")
		}

		// Verify local files NOT removed on API failure
		if _, err := os.Stat(agentlinkDir); err != nil {
			t.Error("expected .agentlink directory to remain after API failure")
		}
	})

	t.Run("uninstall missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunUninstall()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})
}

func TestRunAttach_errors(t *testing.T) {
	t.Run("attach missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunAttach("main")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})

	t.Run("attach directory not found", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		agentlinkDir := filepath.Join(homeDir, ".agentlink")
		os.MkdirAll(agentlinkDir, 0755)
		api.WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), "http://localhost:1", "test-device", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_test"}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

		err := RunAttach("nonexistent")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %s", err)
		}
	})
}

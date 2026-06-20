package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupAgentEnv creates an isolated agent environment: config, credentials,
// session dir, and returns the session dir path for chdir.
func setupAgentEnv(t *testing.T, serverURL string) string {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// ~/.agentlink/config.toml
	agentlinkDir := filepath.Join(homeDir, ".agentlink")
	os.MkdirAll(agentlinkDir, 0755)
	WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), serverURL, "test-device", homeDir, "claude", false, nil)

	// ~/.agentlink/credentials.json
	creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
	credData, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

	// worker/.agentlink.toml
	sessionDir := filepath.Join(homeDir, "worker")
	os.MkdirAll(sessionDir, 0755)
	WriteSessionTOML(filepath.Join(sessionDir, ".agentlink.toml"), "worker", "test-device")

	return sessionDir
}

func TestRunSend(t *testing.T) {
	var captured struct {
		to          string
		fromSession string
		content     string
		authHeader  string
	}

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.authHeader = r.Header.Get("Authorization")

		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		r.Body.Close()
		captured.to = req["to"]
		captured.fromSession = req["from_session"]
		captured.content = req["content"]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "test-msg-id"})
	}))
	defer mockSrv.Close()

	sessionDir := setupAgentEnv(t, mockSrv.URL)

	t.Run("send with short name", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunSend("worker", "hello", false); err != nil {
			t.Fatal(err)
		}

		if captured.to != "test-device:worker" {
			t.Errorf("expected to=test-device:worker, got %s", captured.to)
		}
		if captured.fromSession != "worker" {
			t.Errorf("expected from_session=worker, got %s", captured.fromSession)
		}
		if captured.content != "hello" {
			t.Errorf("expected content=hello, got %s", captured.content)
		}
		if captured.authHeader == "" {
			t.Error("expected auth header")
		}
	})

	t.Run("send with full name", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunSend("other-dev:reviewer", "hi there", false); err != nil {
			t.Fatal(err)
		}

		if captured.to != "other-dev:reviewer" {
			t.Errorf("expected to=other-dev:reviewer, got %s", captured.to)
		}
		if captured.content != "hi there" {
			t.Errorf("expected content=hi there, got %s", captured.content)
		}
	})

	t.Run("send with multi-line content", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		content := "line one\nline two\nline three"
		if err := RunSend("worker", content, false); err != nil {
			t.Fatal(err)
		}
		if captured.content != content {
			t.Errorf("content mismatch:\nexpected: %q\ngot: %q", content, captured.content)
		}
	})
}

func TestRunSend_errors(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunSend("worker", "hi", false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config file error, got: %s", err)
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		os.MkdirAll(filepath.Join(homeDir, ".agentlink"), 0755)
		WriteConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir, "claude", false, nil)

		err := RunSend("worker", "hi", false)
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
		WriteConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://localhost:1", "test-dev", homeDir, "claude", false, nil)
		creds := map[string]string{"api_key": "sk_live_test"}
		credData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(filepath.Join(homeDir, ".agentlink", "credentials.json"), credData, 0600)

		err := RunSend("worker", "hi", false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ".agentlink.toml not found") {
			t.Errorf("expected .agentlink.toml error, got: %s", err)
		}
	})

	t.Run("server returns error", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
		}))
		defer mockSrv.Close()

		sessionDir := setupAgentEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunSend("worker", "hi", false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "bad request") {
			t.Errorf("expected server error, got: %s", err)
		}
	})
}

func TestRunPull(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request params
		session := r.URL.Query().Get("session")
		limit := r.URL.Query().Get("limit")
		auth := r.Header.Get("Authorization")

		if session != "worker" {
			t.Errorf("expected session=worker, got %s", session)
		}
		if auth == "" {
			t.Error("expected auth header")
		}

		w.Header().Set("Content-Type", "application/json")

		if limit == "10" {
			// Return 2 messages for --all
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]string{
					{"id": "1", "type": "msg", "from_device": "dev-a", "from_session": "main", "content": "first msg", "created_at": "2026-01-01T00:00:00Z"},
					{"id": "2", "type": "msg", "from_device": "dev-b", "from_session": "reviewer", "content": "second msg", "created_at": "2026-01-02T00:00:00Z"},
				},
			})
		} else if limit == "1" {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]string{
					{"id": "1", "type": "msg", "from_device": "dev-a", "from_session": "main", "content": "single msg", "created_at": "2026-01-01T00:00:00Z"},
				},
			})
		}
	}))
	defer mockSrv.Close()

	t.Run("pull single message", func(t *testing.T) {
		sessionDir := setupAgentEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunPull(false); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("pull all messages", func(t *testing.T) {
		sessionDir := setupAgentEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunPull(true); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRunPull_empty(t *testing.T) {
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer mockSrv.Close()

	sessionDir := setupAgentEnv(t, mockSrv.URL)
	origWd, _ := os.Getwd()
	os.Chdir(sessionDir)
	defer os.Chdir(origWd)

	if err := RunPull(false); err != nil {
		t.Fatal(err)
	}
}

func TestRunPull_errors(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunPull(false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		}))
		defer mockSrv.Close()

		sessionDir := setupAgentEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunPull(false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("expected 401 error, got: %s", err)
		}
	})
}

func TestReadTOML(t *testing.T) {
	content := `
server = "http://example.com"
device = "my-device"
base_dir = "/home/user/agent_team"
`
	if v := ReadTOML(content, "server"); v != "http://example.com" {
		t.Errorf("expected http://example.com, got %q", v)
	}
	if v := ReadTOML(content, "device"); v != "my-device" {
		t.Errorf("expected my-device, got %q", v)
	}
	if v := ReadTOML(content, "base_dir"); v != "/home/user/agent_team" {
		t.Errorf("expected /home/user/agent_team, got %q", v)
	}
	if v := ReadTOML(content, "nonexistent"); v != "" {
		t.Errorf("expected empty for nonexistent key, got %q", v)
	}
}

func TestFindCurrentSession(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b", "c")
	os.MkdirAll(sub, 0755)

	// Place .agentlink.toml at dir/a/b/
	WriteSessionTOML(filepath.Join(dir, "a", "b", ".agentlink.toml"), "my-session", "dev")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(sub)

	session, err := FindCurrentSession()
	if err != nil {
		t.Fatal(err)
	}
	if session != "my-session" {
		t.Errorf("expected my-session, got %s", session)
	}
}

func TestFindCurrentSession_notFound(t *testing.T) {
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(dir)

	_, err := FindCurrentSession()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), ".agentlink.toml not found") {
		t.Errorf("expected not found error, got: %s", err)
	}
}

func TestLoadConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	os.MkdirAll(filepath.Join(homeDir, ".agentlink"), 0755)
	WriteConfigTOML(filepath.Join(homeDir, ".agentlink", "config.toml"), "http://srv:8080", "test-dev", "/tmp", "claude", false, nil)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "http://srv:8080" {
		t.Errorf("expected http://srv:8080, got %s", cfg.Server)
	}
	if cfg.Device != "test-dev" {
		t.Errorf("expected test-dev, got %s", cfg.Device)
	}
}

func TestLoadConfig_missing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadCredentials(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	os.MkdirAll(filepath.Join(homeDir, ".agentlink"), 0755)
	creds := map[string]string{"api_key": "sk_live_testkey123"}
	data, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(filepath.Join(homeDir, ".agentlink", "credentials.json"), data, 0600)

	c, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "sk_live_testkey123" {
		t.Errorf("expected sk_live_testkey123, got %s", c.APIKey)
	}
}

func TestLoadCredentials_missing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	_, err := LoadCredentials()
	if err == nil {
		t.Fatal("expected error")
	}
}

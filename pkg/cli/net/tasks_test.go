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

func setupTaskEnv(t *testing.T, serverURL string) string {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	agentlinkDir := filepath.Join(homeDir, ".agentlink")
	os.MkdirAll(agentlinkDir, 0755)
	WriteConfigTOML(filepath.Join(agentlinkDir, "config.toml"), serverURL, "test-device", homeDir, "claude", false, nil)

	creds := map[string]string{"api_key": "sk_live_" + strings.Repeat("a", 64)}
	credData, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(filepath.Join(agentlinkDir, "credentials.json"), credData, 0600)

	sessionDir := filepath.Join(homeDir, "worker")
	os.MkdirAll(sessionDir, 0755)
	WriteSessionTOML(filepath.Join(sessionDir, ".agentlink.toml"), "worker", "test-device")

	return sessionDir
}

func TestRunTaskSend(t *testing.T) {
	var captured struct {
		to          string
		fromSession string
		taskID      string
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
		captured.taskID = req["task_id"]
		captured.content = req["content"]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "test-msg-id"})
	}))
	defer mockSrv.Close()

	sessionDir := setupTaskEnv(t, mockSrv.URL)

	t.Run("send with short name", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			taskID      string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskSend("worker", "001", "fix login bug", false, ""); err != nil {
			t.Fatal(err)
		}

		if captured.to != "test-device:worker" {
			t.Errorf("expected to=test-device:worker, got %s", captured.to)
		}
		if captured.fromSession != "worker" {
			t.Errorf("expected from_session=worker, got %s", captured.fromSession)
		}
		if captured.taskID != "001" {
			t.Errorf("expected task_id=001, got %s", captured.taskID)
		}
		if captured.content != "fix login bug" {
			t.Errorf("expected content=fix login bug, got %s", captured.content)
		}
		if captured.authHeader == "" {
			t.Error("expected auth header")
		}
	})

	t.Run("send with full name", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			taskID      string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskSend("other-dev:reviewer", "002", "review code", false, ""); err != nil {
			t.Fatal(err)
		}

		if captured.to != "other-dev:reviewer" {
			t.Errorf("expected to=other-dev:reviewer, got %s", captured.to)
		}
		if captured.taskID != "002" {
			t.Errorf("expected task_id=002, got %s", captured.taskID)
		}
		if captured.content != "review code" {
			t.Errorf("expected content=review code, got %s", captured.content)
		}
	})

	t.Run("send with multi-word content", func(t *testing.T) {
		captured = struct {
			to          string
			fromSession string
			taskID      string
			content     string
			authHeader  string
		}{}

		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		content := "fix the login bug and add tests"
		if err := RunTaskSend("worker", "003", content, false, ""); err != nil {
			t.Fatal(err)
		}
		if captured.content != content {
			t.Errorf("content mismatch:\nexpected: %q\ngot: %q", content, captured.content)
		}
	})

	t.Run("send server 409 shows message", func(t *testing.T) {
		mockSrv409 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"error": "target session is busy",
				"recipient_status": map[string]any{
					"device":       "worker-dev",
					"session":      "main",
					"status":       "busy",
					"current_task": "deploy-042",
				},
			})
		}))
		defer mockSrv409.Close()

		sessionDir := setupTaskEnv(t, mockSrv409.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskSend("worker", "001", "test", false, "")
		if err != nil {
			t.Errorf("expected no error for 409 with status, got: %s", err)
		}
	})
}

func TestRunTaskSend_errors(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunTaskSend("worker", "001", "hi", false, "")
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

		err := RunTaskSend("worker", "001", "hi", false, "")
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

		err := RunTaskSend("worker", "001", "hi", false, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ".agentlink.toml not found") {
			t.Errorf("expected .agentlink.toml error, got: %s", err)
		}
	})
}

func TestRunTaskResult(t *testing.T) {
	var captured struct {
		taskID string
		status string
		result string
	}

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		r.Body.Close()
		captured.taskID = req["task_id"]
		captured.status = req["status"]
		captured.result = req["result"]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer mockSrv.Close()

	t.Run("result completed", func(t *testing.T) {
		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskResult("001", "completed", "bug fixed"); err != nil {
			t.Fatal(err)
		}
		if captured.taskID != "001" {
			t.Errorf("expected task_id=001, got %s", captured.taskID)
		}
		if captured.status != "completed" {
			t.Errorf("expected status=completed, got %s", captured.status)
		}
		if captured.result != "bug fixed" {
			t.Errorf("expected result=bug fixed, got %s", captured.result)
		}
	})

	t.Run("result suspended", func(t *testing.T) {
		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskResult("001", "suspended", "need more info"); err != nil {
			t.Fatal(err)
		}
		if captured.status != "suspended" {
			t.Errorf("expected status=suspended, got %s", captured.status)
		}
		if captured.result != "need more info" {
			t.Errorf("expected result=need more info, got %s", captured.result)
		}
	})

	t.Run("result not found", func(t *testing.T) {
		mockSrv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}))
		defer mockSrv404.Close()

		sessionDir := setupTaskEnv(t, mockSrv404.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskResult("999", "completed", "x")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected 404 error, got: %s", err)
		}
	})
}

func TestRunTaskResume(t *testing.T) {
	var captured struct {
		taskID  string
		content string
	}

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		r.Body.Close()
		captured.taskID = req["task_id"]
		captured.content = req["content"]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer mockSrv.Close()

	t.Run("resume success", func(t *testing.T) {
		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskResume("001", "new guidance: do X first"); err != nil {
			t.Fatal(err)
		}
		if captured.taskID != "001" {
			t.Errorf("expected task_id=001, got %s", captured.taskID)
		}
		if captured.content != "new guidance: do X first" {
			t.Errorf("expected content mismatch, got %s", captured.content)
		}
	})

	t.Run("resume not found", func(t *testing.T) {
		mockSrv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}))
		defer mockSrv404.Close()

		sessionDir := setupTaskEnv(t, mockSrv404.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskResume("999", "new guidance")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRunTaskCancel(t *testing.T) {
	var capturedTaskID string

	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		r.Body.Close()
		capturedTaskID = req["task_id"]

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer mockSrv.Close()

	t.Run("cancel success", func(t *testing.T) {
		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskCancel("001"); err != nil {
			t.Fatal(err)
		}
		if capturedTaskID != "001" {
			t.Errorf("expected task_id=001, got %s", capturedTaskID)
		}
	})

	t.Run("cancel not found", func(t *testing.T) {
		mockSrv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}))
		defer mockSrv404.Close()

		sessionDir := setupTaskEnv(t, mockSrv404.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskCancel("999")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRunTaskStatus(t *testing.T) {
	t.Run("status success", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			taskID := r.URL.Query().Get("task_id")
			if taskID != "001" {
				t.Errorf("expected task_id=001, got %s", taskID)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"task_id":      "001",
				"status":       "completed",
				"assigned_to":  "device:worker",
				"issued_by":    "device:main",
				"content":      "fix login bug",
				"result":       "bug fixed",
				"issued_at":    "2026-05-03T12:00:00Z",
				"completed_at": "2026-05-03T12:30:00Z",
			})
		}))
		defer mockSrv.Close()

		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskStatus("001"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("status not found", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
		}))
		defer mockSrv.Close()

		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskStatus("999")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected not found error, got: %s", err)
		}
	})

	t.Run("status server error", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
		}))
		defer mockSrv.Close()

		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		err := RunTaskStatus("001")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
func TestRunTaskList(t *testing.T) {
	t.Run("list with tasks", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/tasks/list" {
				t.Errorf("expected /tasks/list, got %s", r.URL.Path)
			}
			if r.URL.Query().Get("session") != "worker" {
				t.Errorf("expected session=worker, got %s", r.URL.Query().Get("session"))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]string{
					{"task_id": "t-1", "status": "issued", "assigned_to": "dev:worker", "issued_by": "dev:main", "content": "task one", "issued_at": "2026-01-01T00:00:00Z"},
					{"task_id": "t-2", "status": "in_progress", "assigned_to": "dev:worker", "issued_by": "dev:main", "content": "task two", "issued_at": "2026-01-01T00:01:00Z"},
				},
			})
		}))
		defer mockSrv.Close()

		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskList(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("list empty", func(t *testing.T) {
		mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
		}))
		defer mockSrv.Close()

		sessionDir := setupTaskEnv(t, mockSrv.URL)
		origWd, _ := os.Getwd()
		os.Chdir(sessionDir)
		defer os.Chdir(origWd)

		if err := RunTaskList(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("list missing config", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		err := RunTaskList()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "config file not found") {
			t.Errorf("expected config error, got: %s", err)
		}
	})
}

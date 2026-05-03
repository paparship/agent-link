package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTasks(t *testing.T) {
	ctx := context.Background()

	// Register device for task tests
	regBody := `{"device":"task-test","sessions":["main","worker"],"register_password":"test-password"}`
	resp, err := http.Post(ts.URL+"/agents/register", "application/json", strings.NewReader(regBody))
	if err != nil {
		t.Fatal(err)
	}
	var regResp RegisterResponse
	json.NewDecoder(resp.Body).Decode(&regResp)
	resp.Body.Close()
	apiKey := regResp.APIKey

	doReq := func(method, path, body string) (*http.Response, error) {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultClient.Do(req)
	}

	apiKeyHash := sha256.Sum256([]byte(apiKey))
	apiKeyHashHex := hex.EncodeToString(apiKeyHash[:])

	// Cleanup all task data after suite
	t.Cleanup(func() {
		testRdb.Del(ctx, "device:task-test")
		testRdb.Del(ctx, "api_key:"+apiKeyHashHex)
		for _, pattern := range []string{"task:*", "tasks:*", "inbox:task-test:*"} {
			keys, _ := testRdb.Keys(ctx, pattern).Result()
			testRdb.Del(ctx, keys...)
		}
	})

	// Clean inbox between subtests
	cleanInbox := func() {
		testRdb.Del(ctx, "inbox:task-test:worker", "inbox:task-test:main")
	}

	// ===================== POST /tasks/send =====================

	t.Run("send success", func(t *testing.T) {
		cleanInbox()

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-send-ok","content":"fix login bug"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var sr SendResponse
		json.NewDecoder(resp.Body).Decode(&sr)
		if sr.ID == "" {
			t.Error("expected non-empty msg_id")
		}
		if len(sr.ID) != 32 {
			t.Errorf("expected id length 32, got %d", len(sr.ID))
		}

		// Verify Redis task record
		status, _ := testRdb.HGet(ctx, "task:t-send-ok", "status").Result()
		if status != "issued" {
			t.Errorf("expected status=issued, got %s", status)
		}
		assignedTo, _ := testRdb.HGet(ctx, "task:t-send-ok", "assigned_to").Result()
		if assignedTo != "task-test:worker" {
			t.Errorf("expected assigned_to=task-test:worker, got %s", assignedTo)
		}
		issuedBy, _ := testRdb.HGet(ctx, "task:t-send-ok", "issued_by").Result()
		if issuedBy != "task-test:main" {
			t.Errorf("expected issued_by=task-test:main, got %s", issuedBy)
		}
		content, _ := testRdb.HGet(ctx, "task:t-send-ok", "content").Result()
		if content != "fix login bug" {
			t.Errorf("content mismatch: %s", content)
		}
		ttl, _ := testRdb.TTL(ctx, "task:t-send-ok").Result()
		if ttl <= 0 {
			t.Error("expected positive TTL for task record")
		}

		// Verify tracking set
		members, _ := testRdb.SMembers(ctx, "tasks:task-test:worker").Result()
		if !containsStr(members, "t-send-ok") {
			t.Error("task should be in tracking set")
		}

		// Verify inbox item
		data, _ := testRdb.LIndex(ctx, "inbox:task-test:worker", 0).Result()
		var msg Message
		json.Unmarshal([]byte(data), &msg)
		if msg.Type != "task" {
			t.Errorf("expected type=task, got %s", msg.Type)
		}
		if msg.TaskID != "t-send-ok" {
			t.Errorf("expected task_id=t-send-ok, got %s", msg.TaskID)
		}
		if msg.Content != "fix login bug" {
			t.Errorf("expected content=fix login bug, got %s", msg.Content)
		}

		// Cleanup
		testRdb.Del(ctx, "task:t-send-ok")
		testRdb.Del(ctx, "tasks:task-test:worker")
	})

	t.Run("send duplicate task_id", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-dup", "task_id", "t-dup", "status", "issued")
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-dup") })

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-dup","content":"test"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 for duplicate task_id, got %d", resp.StatusCode)
		}
	})

	t.Run("send busy in_progress", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-busy", "task_id", "t-busy", "status", "in_progress")
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-busy")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-busy")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-busy-2","content":"test"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 for in_progress busy, got %d", resp.StatusCode)
		}
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		if !strings.Contains(errResp["error"], "in_progress") {
			t.Errorf("expected in_progress error, got: %s", errResp["error"])
		}
	})

	t.Run("send busy 2 suspended", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-susp-a", "task_id", "t-susp-a", "status", "suspended")
		testRdb.HSet(ctx, "task:t-susp-b", "task_id", "t-susp-b", "status", "suspended")
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-susp-a", "t-susp-b")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-susp-a", "task:t-susp-b")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-susp-blocked","content":"test"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 for 2 suspended, got %d", resp.StatusCode)
		}
	})

	t.Run("send 1 suspended allowed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-susp-1", "task_id", "t-susp-1", "status", "suspended")
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-susp-1")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-susp-1")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-susp-ok","content":"test"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for 1 suspended, got %d", resp.StatusCode)
		}
		testRdb.Del(ctx, "task:t-susp-ok")
	})

	t.Run("send missing fields", func(t *testing.T) {
		cleanInbox()
		tests := []struct {
			name string
			body string
		}{
			{"missing to", `{"from_session":"main","task_id":"x","content":"x"}`},
			{"missing from_session", `{"to":"task-test:worker","task_id":"x","content":"x"}`},
			{"missing task_id", `{"to":"task-test:worker","from_session":"main","content":"x"}`},
			{"missing content", `{"to":"task-test:worker","from_session":"main","task_id":"x"}`},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				resp, err := doReq("POST", "/tasks/send", tc.body)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("expected 400, got %d", resp.StatusCode)
				}
			})
		}
	})

	t.Run("send content too long", func(t *testing.T) {
		cleanInbox()
		content := strings.Repeat("x", 3001)
		body := fmt.Sprintf(`{"to":"task-test:worker","from_session":"main","task_id":"t-long","content":%q}`, content)
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for content>3000, got %d", resp.StatusCode)
		}
	})

	t.Run("send content exactly 3000", func(t *testing.T) {
		cleanInbox()
		content := strings.Repeat("x", 3000)
		body := fmt.Sprintf(`{"to":"task-test:worker","from_session":"main","task_id":"t-3000","content":%q}`, content)
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for 3000-char content, got %d", resp.StatusCode)
		}
		testRdb.Del(ctx, "task:t-3000")
	})

	t.Run("send target device not found", func(t *testing.T) {
		cleanInbox()
		body := `{"to":"no-such-device:main","from_session":"main","task_id":"t-nodev","content":"x"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("send target session not found", func(t *testing.T) {
		cleanInbox()
		body := `{"to":"task-test:no-such-session","from_session":"main","task_id":"t-nosess","content":"x"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("send no auth", func(t *testing.T) {
		cleanInbox()
		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-noauth","content":"x"}`
		resp, err := http.Post(ts.URL+"/tasks/send", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("send invalid task_id", func(t *testing.T) {
		cleanInbox()
		body := `{"to":"task-test:worker","from_session":"main","task_id":"INVALID","content":"x"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid task_id, got %d", resp.StatusCode)
		}
	})

	// ===================== GET /inbox/pull task awareness =====================

	t.Run("pull task auto in_progress", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-pull-auto", "task_id", "t-pull-auto", "status", "issued")
		msg := Message{ID: "pull-1", Type: "task", FromDevice: "task-test", FromSession: "main", TaskID: "t-pull-auto", Content: "do something", CreatedAt: "2026-01-01T00:00:00Z"}
		data, _ := json.Marshal(msg)
		testRdb.LPush(ctx, "inbox:task-test:worker", data)
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-pull-auto", "tasks:task-test:worker")
			testRdb.Del(ctx, "inbox:task-test:worker")
		})

		resp, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var pr PullResponse
		json.NewDecoder(resp.Body).Decode(&pr)
		if len(pr.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(pr.Items))
		}
		if pr.Items[0].Type != "task" {
			t.Errorf("expected type=task, got %s", pr.Items[0].Type)
		}
		if pr.Items[0].TaskID != "t-pull-auto" {
			t.Errorf("expected task_id=t-pull-auto, got %s", pr.Items[0].TaskID)
		}

		// Verify task status changed to in_progress
		status, _ := testRdb.HGet(ctx, "task:t-pull-auto", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected status=in_progress after pull, got %s", status)
		}
	})

	t.Run("pull msg no side effect", func(t *testing.T) {
		cleanInbox()
		msg := Message{ID: "plain-msg", Type: "msg", FromDevice: "task-test", FromSession: "main", Content: "hello", CreatedAt: "2026-01-01T00:00:00Z"}
		data, _ := json.Marshal(msg)
		testRdb.LPush(ctx, "inbox:task-test:worker", data)
		t.Cleanup(func() { testRdb.Del(ctx, "inbox:task-test:worker") })

		resp, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var pr PullResponse
		json.NewDecoder(resp.Body).Decode(&pr)
		if len(pr.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(pr.Items))
		}
		if pr.Items[0].Type != "msg" {
			t.Errorf("expected type=msg, got %s", pr.Items[0].Type)
		}
	})

	// ===================== POST /tasks/result =====================

	t.Run("result completed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-res-ok",
			"task_id", "t-res-ok", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test", "issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-res-ok")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-res-ok")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"task_id":"t-res-ok","status":"completed","result":"bug fixed"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Verify task state
		status, _ := testRdb.HGet(ctx, "task:t-res-ok", "status").Result()
		if status != "completed" {
			t.Errorf("expected status=completed, got %s", status)
		}
		result, _ := testRdb.HGet(ctx, "task:t-res-ok", "result").Result()
		if result != "bug fixed" {
			t.Errorf("expected result=bug fixed, got %s", result)
		}
		completedAt, _ := testRdb.HGet(ctx, "task:t-res-ok", "completed_at").Result()
		if completedAt == "" {
			t.Error("completed_at should not be empty")
		}

		// Verify removed from tracking set
		members, _ := testRdb.SMembers(ctx, "tasks:task-test:worker").Result()
		if containsStr(members, "t-res-ok") {
			t.Error("task should be removed from tracking set on complete")
		}
	})

	t.Run("result suspended", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-res-susp",
			"task_id", "t-res-susp", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-res-susp")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-res-susp")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"task_id":"t-res-susp","status":"suspended","result":"need more info"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		status, _ := testRdb.HGet(ctx, "task:t-res-susp", "status").Result()
		if status != "suspended" {
			t.Errorf("expected status=suspended, got %s", status)
		}
		result, _ := testRdb.HGet(ctx, "task:t-res-susp", "result").Result()
		if result != "need more info" {
			t.Errorf("expected result=need more info, got %s", result)
		}

		// Verify removed from tracking set
		members, _ := testRdb.SMembers(ctx, "tasks:task-test:worker").Result()
		if containsStr(members, "t-res-susp") {
			t.Error("task should be removed from tracking set on suspend")
		}
	})

	t.Run("result task not found", func(t *testing.T) {
		body := `{"task_id":"t-nonexistent","status":"completed","result":"done"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("result invalid status", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-res-bad", "status", "in_progress")
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-res-bad") })

		body := `{"task_id":"t-res-bad","status":"invalid","result":"x"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("result not in_progress", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-res-nip", "status", "issued")
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-res-nip") })

		body := `{"task_id":"t-res-nip","status":"completed","result":"x"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for non-in_progress task, got %d", resp.StatusCode)
		}
	})

	t.Run("result no auth", func(t *testing.T) {
		body := `{"task_id":"x","status":"completed","result":"x"}`
		resp, err := http.Post(ts.URL+"/tasks/result", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	// ===================== POST /tasks/resume =====================

	t.Run("resume success", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-resume",
			"task_id", "t-resume", "status", "suspended",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "original task", "result", "blocked",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-resume") })

		body := `{"task_id":"t-resume","content":"new guidance: do X first"}`
		resp, err := doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Verify task state
		status, _ := testRdb.HGet(ctx, "task:t-resume", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected status=in_progress, got %s", status)
		}
		content, _ := testRdb.HGet(ctx, "task:t-resume", "content").Result()
		if content != "new guidance: do X first" {
			t.Errorf("expected updated content, got %s", content)
		}
		result, _ := testRdb.HGet(ctx, "task:t-resume", "result").Result()
		if result != "" {
			t.Errorf("expected result cleared, got %s", result)
		}

		// Verify re-added to tracking set
		members, _ := testRdb.SMembers(ctx, "tasks:task-test:worker").Result()
		if !containsStr(members, "t-resume") {
			t.Error("task should be re-added to tracking set on resume")
		}
		testRdb.Del(ctx, "tasks:task-test:worker")
		testRdb.Del(ctx, "inbox:task-test:worker")
	})

	t.Run("resume not found", func(t *testing.T) {
		body := `{"task_id":"t-resume-none","content":"new guidance"}`
		resp, err := doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("resume not suspended", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-resume-ns", "status", "in_progress")
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-resume-ns") })

		body := `{"task_id":"t-resume-ns","content":"new guidance"}`
		resp, err := doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for non-suspended task, got %d", resp.StatusCode)
		}
	})

	t.Run("resume no auth", func(t *testing.T) {
		body := `{"task_id":"x","content":"new guidance"}`
		resp, err := http.Post(ts.URL+"/tasks/resume", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	// ===================== POST /tasks/cancel =====================

	t.Run("cancel success", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-cancel",
			"task_id", "t-cancel", "status", "issued",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "tasks:task-test:worker", "t-cancel")
		t.Cleanup(func() {
			testRdb.Del(ctx, "task:t-cancel")
			testRdb.Del(ctx, "tasks:task-test:worker")
		})

		body := `{"task_id":"t-cancel"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		status, _ := testRdb.HGet(ctx, "task:t-cancel", "status").Result()
		if status != "cancelled" {
			t.Errorf("expected status=cancelled, got %s", status)
		}
		completedAt, _ := testRdb.HGet(ctx, "task:t-cancel", "completed_at").Result()
		if completedAt == "" {
			t.Error("completed_at should not be empty")
		}

		members, _ := testRdb.SMembers(ctx, "tasks:task-test:worker").Result()
		if containsStr(members, "t-cancel") {
			t.Error("task should be removed from tracking set on cancel")
		}
	})

	t.Run("cancel already completed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-cancel-done", "status", "completed")
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-cancel-done") })

		body := `{"task_id":"t-cancel-done"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for completed task, got %d", resp.StatusCode)
		}
	})

	t.Run("cancel not found", func(t *testing.T) {
		body := `{"task_id":"t-cancel-none"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("cancel no auth", func(t *testing.T) {
		body := `{"task_id":"x"}`
		resp, err := http.Post(ts.URL+"/tasks/cancel", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	// ===================== GET /tasks/status =====================

	t.Run("status success", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "task:t-status-ok",
			"task_id", "t-status-ok", "status", "completed",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "fix bug", "result", "done",
			"issued_at", "2026-01-01T00:00:00Z",
			"completed_at", "2026-01-01T01:00:00Z",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "task:t-status-ok") })

		resp, err := doReq("GET", "/tasks/status?task_id=t-status-ok", "")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var tsResp TaskStatusResponse
		json.NewDecoder(resp.Body).Decode(&tsResp)
		if tsResp.TaskID != "t-status-ok" {
			t.Errorf("expected task_id=t-status-ok, got %s", tsResp.TaskID)
		}
		if tsResp.Status != "completed" {
			t.Errorf("expected status=completed, got %s", tsResp.Status)
		}
		if tsResp.AssignedTo != "task-test:worker" {
			t.Errorf("expected assigned_to=task-test:worker, got %s", tsResp.AssignedTo)
		}
		if tsResp.IssuedBy != "task-test:main" {
			t.Errorf("expected issued_by=task-test:main, got %s", tsResp.IssuedBy)
		}
		if tsResp.Content != "fix bug" {
			t.Errorf("expected content=fix bug, got %s", tsResp.Content)
		}
		if tsResp.Result != "done" {
			t.Errorf("expected result=done, got %s", tsResp.Result)
		}
		if tsResp.IssuedAt != "2026-01-01T00:00:00Z" {
			t.Errorf("issued_at mismatch: %s", tsResp.IssuedAt)
		}
		if tsResp.CompletedAt != "2026-01-01T01:00:00Z" {
			t.Errorf("completed_at mismatch: %s", tsResp.CompletedAt)
		}
	})

	t.Run("status not found", func(t *testing.T) {
		resp, err := doReq("GET", "/tasks/status?task_id=t-status-none", "")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("status no auth", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/tasks/status?task_id=x")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	// ===================== Full lifecycle =====================

	t.Run("lifecycle send pull complete", func(t *testing.T) {
		cleanInbox()

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-life-1","content":"fix login"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		resp, err = doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		var pr PullResponse
		json.NewDecoder(resp.Body).Decode(&pr)
		resp.Body.Close()
		if len(pr.Items) != 1 {
			t.Fatalf("expected 1 item from pull, got %d", len(pr.Items))
		}

		status, _ := testRdb.HGet(ctx, "task:t-life-1", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected in_progress after pull, got %s", status)
		}

		body = `{"task_id":"t-life-1","status":"completed","result":"fixed"}`
		resp, err = doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ = testRdb.HGet(ctx, "task:t-life-1", "status").Result()
		if status != "completed" {
			t.Errorf("expected completed, got %s", status)
		}

		resp, err = doReq("GET", "/tasks/status?task_id=t-life-1", "")
		if err != nil {
			t.Fatal(err)
		}
		var tsResp TaskStatusResponse
		json.NewDecoder(resp.Body).Decode(&tsResp)
		resp.Body.Close()
		if tsResp.Status != "completed" {
			t.Errorf("expected status=completed, got %s", tsResp.Status)
		}
		if tsResp.Result != "fixed" {
			t.Errorf("expected result=fixed, got %s", tsResp.Result)
		}

		testRdb.Del(ctx, "task:t-life-1")
	})

	t.Run("lifecycle suspend resume complete", func(t *testing.T) {
		cleanInbox()

		doReq("POST", "/tasks/send", `{"to":"task-test:worker","from_session":"main","task_id":"t-life-2","content":"refactor auth"}`)

		resp, _ := doReq("GET", "/inbox/pull?session=worker", "")
		resp.Body.Close()

		body := `{"task_id":"t-life-2","status":"suspended","result":"need spec"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ := testRdb.HGet(ctx, "task:t-life-2", "status").Result()
		if status != "suspended" {
			t.Errorf("expected suspended, got %s", status)
		}

		body = `{"task_id":"t-life-2","content":"new spec: add OAuth"}`
		resp, err = doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ = testRdb.HGet(ctx, "task:t-life-2", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected in_progress after resume, got %s", status)
		}

		resp, err = doReq("GET", "/inbox/pull?session=worker", "")
		if err != nil {
			t.Fatal(err)
		}
		var pr PullResponse
		json.NewDecoder(resp.Body).Decode(&pr)
		resp.Body.Close()
		if len(pr.Items) != 1 {
			t.Errorf("expected 1 item after resume pull, got %d", len(pr.Items))
		} else if pr.Items[0].Content != "new spec: add OAuth" {
			t.Errorf("expected updated content in inbox, got %s", pr.Items[0].Content)
		}

		body = `{"task_id":"t-life-2","status":"completed","result":"OAuth implemented"}`
		resp, err = doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ = testRdb.HGet(ctx, "task:t-life-2", "status").Result()
		if status != "completed" {
			t.Errorf("expected completed, got %s", status)
		}

		testRdb.Del(ctx, "task:t-life-2")
		testRdb.Del(ctx, "inbox:task-test:worker")
		testRdb.Del(ctx, "tasks:task-test:worker")
	})

	t.Run("lifecycle send cancel", func(t *testing.T) {
		cleanInbox()

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-life-3","content":"risky change"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		body = `{"task_id":"t-life-3"}`
		resp, err = doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ := testRdb.HGet(ctx, "task:t-life-3", "status").Result()
		if status != "cancelled" {
			t.Errorf("expected cancelled, got %s", status)
		}

		testRdb.Del(ctx, "task:t-life-3")
	})
}

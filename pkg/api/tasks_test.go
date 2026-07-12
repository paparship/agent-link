package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return string(b)
}

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
		testRdb.Del(ctx, "agentlink:device:task-test")
		testRdb.Del(ctx, "agentlink:api_key:"+apiKeyHashHex)
		for _, pattern := range []string{"agentlink:task:*", "agentlink:tasks:*", "agentlink:issued:*", "agentlink:inbox:task-test:*"} {
			keys, _ := testRdb.Keys(ctx, pattern).Result()
			testRdb.Del(ctx, keys...)
		}
	})

	// Clean inbox between subtests
	cleanInbox := func() {
		testRdb.Del(ctx, "agentlink:inbox:task-test:worker", "agentlink:inbox:task-test:main")
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
		status, _ := testRdb.HGet(ctx, "agentlink:task:t-send-ok", "status").Result()
		if status != "issued" {
			t.Errorf("expected status=issued, got %s", status)
		}
		assignedTo, _ := testRdb.HGet(ctx, "agentlink:task:t-send-ok", "assigned_to").Result()
		if assignedTo != "task-test:worker" {
			t.Errorf("expected assigned_to=task-test:worker, got %s", assignedTo)
		}
		issuedBy, _ := testRdb.HGet(ctx, "agentlink:task:t-send-ok", "issued_by").Result()
		if issuedBy != "task-test:main" {
			t.Errorf("expected issued_by=task-test:main, got %s", issuedBy)
		}
		content, _ := testRdb.HGet(ctx, "agentlink:task:t-send-ok", "content").Result()
		if content != "fix login bug" {
			t.Errorf("content mismatch: %s", content)
		}
		title, _ := testRdb.HGet(ctx, "agentlink:task:t-send-ok", "title").Result()
		if title != "t-send-ok" {
			t.Errorf("expected default title=t-send-ok, got %s", title)
		}
		ttl, _ := testRdb.TTL(ctx, "agentlink:task:t-send-ok").Result()
		if ttl <= 0 {
			t.Error("expected positive TTL for task record")
		}

		// Verify tracking set
		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if !containsStr(members, "t-send-ok") {
			t.Error("task should be in tracking set")
		}

		// Verify inbox item
		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		var msg Message
		json.Unmarshal([]byte(data), &msg)
		if msg.Type != "task" {
			t.Errorf("expected type=task, got %s", msg.Type)
		}
		if msg.TaskID != "t-send-ok" {
			t.Errorf("expected task_id=t-send-ok, got %s", msg.TaskID)
		}
		if msg.Title != "t-send-ok" {
			t.Errorf("expected inbox title=t-send-ok, got %s", msg.Title)
		}
		if msg.Content != "fix login bug" {
			t.Errorf("expected content=fix login bug, got %s", msg.Content)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:task:t-send-ok")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	t.Run("send with explicit title", func(t *testing.T) {
		cleanInbox()

		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-title","title":"诊断登录bug","content":"fix login bug"}`
		resp, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		title, _ := testRdb.HGet(ctx, "agentlink:task:t-title", "title").Result()
		if title != "诊断登录bug" {
			t.Errorf("expected title=诊断登录bug, got %s", title)
		}

		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		var msg Message
		json.Unmarshal([]byte(data), &msg)
		if msg.Title != "诊断登录bug" {
			t.Errorf("expected inbox title=诊断登录bug, got %s", msg.Title)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:task:t-title")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	t.Run("send duplicate task_id", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-dup", "task_id", "t-dup", "status", "issued")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-dup") })

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
		testRdb.HSet(ctx, "agentlink:task:t-busy", "task_id", "t-busy", "status", "in_progress")
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-busy")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-busy")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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
		var errResp struct {
			Error           string `json:"error"`
			RecipientStatus any    `json:"recipient_status"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if !strings.Contains(errResp.Error, "busy") {
			t.Errorf("expected busy error, got: %s", errResp.Error)
		}
		if errResp.RecipientStatus == nil {
			t.Error("expected recipient_status in 409 response")
		}
	})

	t.Run("send busy 2 suspended", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-susp-a", "task_id", "t-susp-a", "status", "suspended")
		testRdb.HSet(ctx, "agentlink:task:t-susp-b", "task_id", "t-susp-b", "status", "suspended")
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-susp-a", "t-susp-b")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-susp-a", "agentlink:task:t-susp-b")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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
		testRdb.HSet(ctx, "agentlink:task:t-susp-1", "task_id", "t-susp-1", "status", "suspended")
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-susp-1")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-susp-1")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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
		testRdb.Del(ctx, "agentlink:task:t-susp-ok")
	})

	t.Run("send missing fields", func(t *testing.T) {
		cleanInbox()
		tests := []struct {
			name string
			body string
		}{
			{"missing to", `{"from_session":"main","task_id":"x","content":"x"}`},
			{"missing from_session", `{"to":"task-test:worker","task_id":"x","content":"x"}`},
			{"invalid task_id format", `{"to":"task-test:worker","from_session":"main","task_id":"UPPERCASE","content":"x"}`},
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
		testRdb.Del(ctx, "agentlink:task:t-3000")
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
		testRdb.HSet(ctx, "agentlink:task:t-pull-auto", "task_id", "t-pull-auto", "status", "issued")
		msg := Message{ID: "pull-1", Type: "task", FromDevice: "task-test", FromSession: "main", TaskID: "t-pull-auto", Content: "do something", CreatedAt: "2026-01-01T00:00:00Z"}
		data, _ := json.Marshal(msg)
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", data)
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-pull-auto", "agentlink:tasks:task-test:worker")
			testRdb.Del(ctx, "agentlink:inbox:task-test:worker")
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
		status, _ := testRdb.HGet(ctx, "agentlink:task:t-pull-auto", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected status=in_progress after pull, got %s", status)
		}
	})

	t.Run("pull skips cancelled task", func(t *testing.T) {
		cleanInbox()
		// Stale inbox item for a cancelled task — should be skipped on pull.
		testRdb.HSet(ctx, "agentlink:task:t-pull-stale",
			"task_id", "t-pull-stale", "status", "cancelled",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
		)
		stale := Message{ID: "stale-1", Type: "task", TaskID: "t-pull-stale", FromDevice: "task-test", FromSession: "main", Content: "stale", CreatedAt: "2026-01-01T00:00:00Z"}
		staleJSON, _ := json.Marshal(stale)
		// Push stale first (LPUSH), then a fresh msg on top — pull RPOPs in order
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", staleJSON)
		fresh := Message{ID: "fresh-1", Type: "msg", FromDevice: "task-test", FromSession: "main", Content: "fresh", CreatedAt: "2026-01-01T00:00:01Z"}
		freshJSON, _ := json.Marshal(fresh)
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", freshJSON)
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-pull-stale", "agentlink:inbox:task-test:worker")
		})

		// limit=1 — fresh msg is at tail (RPOP gets it first), stale skipped would come second
		// Actually: LPush pushes to head, RPop takes from tail. fresh pushed last → at head.
		// Order of RPop: stale first (tail), then fresh.
		// Stale skipped → only fresh returned, limit not consumed by stale.
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
			t.Fatalf("expected 1 item (fresh msg, stale skipped), got %d", len(pr.Items))
		}
		if pr.Items[0].Type != "msg" || pr.Items[0].ID != "fresh-1" {
			t.Errorf("expected fresh msg, got %+v", pr.Items[0])
		}

		// Task record unchanged (still cancelled)
		st, _ := testRdb.HGet(ctx, "agentlink:task:t-pull-stale", "status").Result()
		if st != "cancelled" {
			t.Errorf("expected task still cancelled, got %s", st)
		}
	})

	t.Run("pull skips suspended and completed tasks", func(t *testing.T) {
		cleanInbox()
		// Two stale items (suspended + completed) then a valid task
		testRdb.HSet(ctx, "agentlink:task:t-pull-susp", "task_id", "t-pull-susp", "status", "suspended")
		testRdb.HSet(ctx, "agentlink:task:t-pull-done", "task_id", "t-pull-done", "status", "completed")
		testRdb.HSet(ctx, "agentlink:task:t-pull-good", "task_id", "t-pull-good", "status", "issued")

		susp := Message{ID: "s", Type: "task", TaskID: "t-pull-susp", Content: "s", CreatedAt: "2026-01-01T00:00:00Z"}
		done := Message{ID: "d", Type: "task", TaskID: "t-pull-done", Content: "d", CreatedAt: "2026-01-01T00:00:00Z"}
		good := Message{ID: "g", Type: "task", TaskID: "t-pull-good", Content: "g", CreatedAt: "2026-01-01T00:00:00Z"}
		// Push in reverse so RPop order is: susp, done, good
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", mustJSON(t, good), mustJSON(t, done), mustJSON(t, susp))
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-pull-susp", "agentlink:task:t-pull-done", "agentlink:task:t-pull-good")
			testRdb.Del(ctx, "agentlink:inbox:task-test:worker", "agentlink:tasks:task-test:worker")
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
		if pr.Items[0].TaskID != "t-pull-good" {
			t.Errorf("expected only the issued task returned, got %s", pr.Items[0].TaskID)
		}

		// Skipped tasks untouched; good task now in_progress
		suspSt, _ := testRdb.HGet(ctx, "agentlink:task:t-pull-susp", "status").Result()
		if suspSt != "suspended" {
			t.Errorf("expected suspended task untouched, got %s", suspSt)
		}
		doneSt, _ := testRdb.HGet(ctx, "agentlink:task:t-pull-done", "status").Result()
		if doneSt != "completed" {
			t.Errorf("expected completed task untouched, got %s", doneSt)
		}
		goodSt, _ := testRdb.HGet(ctx, "agentlink:task:t-pull-good", "status").Result()
		if goodSt != "in_progress" {
			t.Errorf("expected issued task now in_progress, got %s", goodSt)
		}
	})

	t.Run("pull msg no side effect", func(t *testing.T) {
		cleanInbox()
		msg := Message{ID: "plain-msg", Type: "msg", FromDevice: "task-test", FromSession: "main", Content: "hello", CreatedAt: "2026-01-01T00:00:00Z"}
		data, _ := json.Marshal(msg)
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", data)
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:inbox:task-test:worker") })

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

	t.Run("pull msg sets current_msg", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:current_msg:task-test:worker")

		msg := Message{ID: "cm-msg", Type: "msg", FromDevice: "task-test", FromSession: "main", Title: "通知标题", Content: "hello", CreatedAt: "2026-01-01T00:00:00Z"}
		data, _ := json.Marshal(msg)
		testRdb.LPush(ctx, "agentlink:inbox:task-test:worker", data)
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:inbox:task-test:worker")
			testRdb.Del(ctx, "agentlink:current_msg:task-test:worker")
		})

		resp, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		title, _ := testRdb.HGet(ctx, "agentlink:current_msg:task-test:worker", "title").Result()
		if title != "通知标题" {
			t.Errorf("expected current_msg title=通知标题, got %q", title)
		}
		startedAt, _ := testRdb.HGet(ctx, "agentlink:current_msg:task-test:worker", "started_at").Result()
		if startedAt == "" {
			t.Error("expected non-empty started_at")
		}
		ttl, _ := testRdb.TTL(ctx, "agentlink:current_msg:task-test:worker").Result()
		if ttl <= 0 || ttl > 10*time.Minute {
			t.Errorf("expected TTL <= 10m, got %v", ttl)
		}
	})

	t.Run("pull empty clears current_msg", func(t *testing.T) {
		cleanInbox()
		// Pre-set current_msg from a prior pull
		testRdb.HSet(ctx, "agentlink:current_msg:task-test:worker", "title", "old", "started_at", "2026-01-01T00:00:00Z")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:current_msg:task-test:worker") })

		resp, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		exists, _ := testRdb.Exists(ctx, "agentlink:current_msg:task-test:worker").Result()
		if exists != 0 {
			t.Error("expected current_msg cleared on empty pull")
		}
	})

	t.Run("pull task does not set current_msg", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:current_msg:task-test:worker")

		// Send a real task so task record exists for auto in_progress
		body := `{"to":"task-test:worker","from_session":"main","task_id":"t-cm-task","content":"do thing"}`
		resp0, err := doReq("POST", "/tasks/send", body)
		if err != nil {
			t.Fatal(err)
		}
		resp0.Body.Close()
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-cm-task")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
			testRdb.Del(ctx, "agentlink:current_msg:task-test:worker")
		})

		resp, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		exists, _ := testRdb.Exists(ctx, "agentlink:current_msg:task-test:worker").Result()
		if exists != 0 {
			t.Error("expected current_msg NOT set for task pull")
		}
	})

	// ===================== POST /tasks/result =====================

	t.Run("result completed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-res-ok",
			"task_id", "t-res-ok", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test", "issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-res-ok")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-res-ok")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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
		status, _ := testRdb.HGet(ctx, "agentlink:task:t-res-ok", "status").Result()
		if status != "completed" {
			t.Errorf("expected status=completed, got %s", status)
		}
		result, _ := testRdb.HGet(ctx, "agentlink:task:t-res-ok", "result").Result()
		if result != "bug fixed" {
			t.Errorf("expected result=bug fixed, got %s", result)
		}
		completedAt, _ := testRdb.HGet(ctx, "agentlink:task:t-res-ok", "completed_at").Result()
		if completedAt == "" {
			t.Error("completed_at should not be empty")
		}

		// Verify removed from tracking set
		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if containsStr(members, "t-res-ok") {
			t.Error("task should be removed from tracking set on complete")
		}
	})

	t.Run("result suspended", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-res-susp",
			"task_id", "t-res-susp", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-res-susp")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-res-susp")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-res-susp", "status").Result()
		if status != "suspended" {
			t.Errorf("expected status=suspended, got %s", status)
		}
		result, _ := testRdb.HGet(ctx, "agentlink:task:t-res-susp", "result").Result()
		if result != "need more info" {
			t.Errorf("expected result=need more info, got %s", result)
		}

		// Verify removed from tracking set
		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if containsStr(members, "t-res-susp") {
			t.Error("task should be removed from tracking set on suspend")
		}
	})

	t.Run("result completed notifies issuer", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-notify-ok",
			"task_id", "t-notify-ok", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test", "issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-notify-ok")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-notify-ok")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
		})

		body := `{"task_id":"t-notify-ok","status":"completed","result":"已清理 2.3G"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Notification lands in issuer's inbox (task-test:main)
		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:main", 0).Result()
		if data == "" {
			t.Fatal("expected notification in issuer inbox, got empty")
		}
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode inbox msg: %v", err)
		}
		if msg.Type != MsgTypeMsg {
			t.Errorf("expected type=msg, got %s", msg.Type)
		}
		if msg.Title != "任务回报 t-notify-ok" {
			t.Errorf("expected title=任务回报 t-notify-ok, got %s", msg.Title)
		}
		if msg.Content != "completed: 已清理 2.3G" {
			t.Errorf("expected content=completed: 已清理 2.3G, got %s", msg.Content)
		}
		if msg.FromDevice != "task-test" || msg.FromSession != "worker" {
			t.Errorf("expected from=task-test:worker, got %s:%s", msg.FromDevice, msg.FromSession)
		}

		// Task record unchanged by notification
		st, _ := testRdb.HGet(ctx, "agentlink:task:t-notify-ok", "status").Result()
		if st != "completed" {
			t.Errorf("expected task still completed, got %s", st)
		}
	})

	t.Run("result suspended notifies issuer", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-notify-susp",
			"task_id", "t-notify-susp", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test", "issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-notify-susp")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-notify-susp")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
		})

		body := `{"task_id":"t-notify-susp","status":"suspended","result":"权限不足"}`
		resp, err := doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:main", 0).Result()
		if data == "" {
			t.Fatal("expected notification in issuer inbox, got empty")
		}
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode inbox msg: %v", err)
		}
		if msg.Title != "任务回报 t-notify-susp" {
			t.Errorf("expected title=任务回报 t-notify-susp, got %s", msg.Title)
		}
		if msg.Content != "suspended: 权限不足" {
			t.Errorf("expected content=suspended: 权限不足, got %s", msg.Content)
		}
	})

	t.Run("cancel notifies target", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-notify-cancel",
			"task_id", "t-notify-cancel", "status", "in_progress",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test", "issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-notify-cancel")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-notify-cancel")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
		})

		body := `{"task_id":"t-notify-cancel"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Cancel notification lands in target's inbox (task-test:worker)
		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		if data == "" {
			t.Fatal("expected cancel notification in target inbox, got empty")
		}
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode inbox msg: %v", err)
		}
		if msg.Type != MsgTypeMsg {
			t.Errorf("expected type=msg, got %s", msg.Type)
		}
		if msg.Title != "任务回报 t-notify-cancel" {
			t.Errorf("expected title=任务回报 t-notify-cancel, got %s", msg.Title)
		}
		if msg.Content != "cancelled" {
			t.Errorf("expected content=cancelled, got %s", msg.Content)
		}
		if msg.FromDevice != "task-test" || msg.FromSession != "main" {
			t.Errorf("expected from=task-test:main (issuer), got %s:%s", msg.FromDevice, msg.FromSession)
		}

		// Task record unchanged by notification
		st, _ := testRdb.HGet(ctx, "agentlink:task:t-notify-cancel", "status").Result()
		if st != "cancelled" {
			t.Errorf("expected task still cancelled, got %s", st)
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
		testRdb.HSet(ctx, "agentlink:task:t-res-bad", "status", "in_progress")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-res-bad") })

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
		testRdb.HSet(ctx, "agentlink:task:t-res-nip", "status", "issued")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-res-nip") })

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
		testRdb.HSet(ctx, "agentlink:task:t-resume",
			"task_id", "t-resume", "status", "suspended",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "original task", "result", "blocked",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-resume") })

		body := `{"task_id":"t-resume","content":"new guidance: do X first"}`
		resp, err := doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Verify task state — resume resets to issued (waiting for pull to promote to in_progress)
		status, _ := testRdb.HGet(ctx, "agentlink:task:t-resume", "status").Result()
		if status != "issued" {
			t.Errorf("expected status=issued, got %s", status)
		}
		content, _ := testRdb.HGet(ctx, "agentlink:task:t-resume", "content").Result()
		if content != "new guidance: do X first" {
			t.Errorf("expected updated content, got %s", content)
		}
		result, _ := testRdb.HGet(ctx, "agentlink:task:t-resume", "result").Result()
		if result != "" {
			t.Errorf("expected result cleared, got %s", result)
		}

		// Verify re-added to tracking set
		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if !containsStr(members, "t-resume") {
			t.Error("task should be re-added to tracking set on resume")
		}
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
		testRdb.Del(ctx, "agentlink:inbox:task-test:worker")
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
		testRdb.HSet(ctx, "agentlink:task:t-resume-ns", "status", "in_progress")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-resume-ns") })

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
		testRdb.HSet(ctx, "agentlink:task:t-cancel",
			"task_id", "t-cancel", "status", "issued",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-cancel")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-cancel")
			testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-cancel", "status").Result()
		if status != "cancelled" {
			t.Errorf("expected status=cancelled, got %s", status)
		}
		completedAt, _ := testRdb.HGet(ctx, "agentlink:task:t-cancel", "completed_at").Result()
		if completedAt == "" {
			t.Error("completed_at should not be empty")
		}

		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if containsStr(members, "t-cancel") {
			t.Error("task should be removed from tracking set on cancel")
		}
	})

	t.Run("cancel issued skips notification", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-cancel-issued",
			"task_id", "t-cancel-issued", "status", "issued",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-cancel-issued")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-cancel-issued", "agentlink:tasks:task-test:worker")
		})

		body := `{"task_id":"t-cancel-issued"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Status moved to cancelled
		st, _ := testRdb.HGet(ctx, "agentlink:task:t-cancel-issued", "status").Result()
		if st != "cancelled" {
			t.Errorf("expected cancelled, got %s", st)
		}

		// Issued-cancel does NOT push a notification — pull-side filtering
		// drops the stale inbox item instead, so the recipient never sees
		// the task at all.
		llen, _ := testRdb.LLen(ctx, "agentlink:inbox:task-test:worker").Result()
		if llen != 0 {
			t.Errorf("expected target inbox empty after issued cancel, got %d items", llen)
		}
	})

	t.Run("cancel suspended notifies target", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-cancel-susp",
			"task_id", "t-cancel-susp", "status", "suspended",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "test",
		)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-cancel-susp")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-cancel-susp", "agentlink:tasks:task-test:worker")
		})

		body := `{"task_id":"t-cancel-susp"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		st, _ := testRdb.HGet(ctx, "agentlink:task:t-cancel-susp", "status").Result()
		if st != "cancelled" {
			t.Errorf("expected cancelled, got %s", st)
		}

		// Suspended-cancel DOES push notification (recipient has context)
		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		if data == "" {
			t.Fatal("expected cancel notification in target inbox, got empty")
		}
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Title != "任务回报 t-cancel-susp" {
			t.Errorf("expected title=任务回报 t-cancel-susp, got %s", msg.Title)
		}
		if msg.Content != "cancelled" {
			t.Errorf("expected content=cancelled, got %s", msg.Content)
		}
	})

	t.Run("cancel already cancelled rejected", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-cancel-twice", "status", "cancelled")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-cancel-twice") })

		body := `{"task_id":"t-cancel-twice"}`
		resp, err := doReq("POST", "/tasks/cancel", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for already-cancelled, got %d", resp.StatusCode)
		}
	})

	t.Run("cancel already completed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-cancel-done", "status", "completed")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-cancel-done") })

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

	// ===================== POST /tasks/reopen =====================

	t.Run("reopen completed", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-reopen-done",
			"task_id", "t-reopen-done", "status", "completed",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "原任务内容", "title", "诊断bug",
			"result", "上次的成果", "completed_at", "2026-01-01T00:00:00Z",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-reopen-done", "agentlink:tasks:task-test:worker") })

		body := `{"task_id":"t-reopen-done","reason":"上次权限不足,现已开通"}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Status reset to issued, result/completed_at cleared
		st, _ := testRdb.HGet(ctx, "agentlink:task:t-reopen-done", "status").Result()
		if st != "issued" {
			t.Errorf("expected status=issued, got %s", st)
		}
		result, _ := testRdb.HGet(ctx, "agentlink:task:t-reopen-done", "result").Result()
		if result != "" {
			t.Errorf("expected result cleared, got %s", result)
		}
		completedAt, _ := testRdb.HGet(ctx, "agentlink:task:t-reopen-done", "completed_at").Result()
		if completedAt != "" {
			t.Errorf("expected completed_at cleared, got %s", completedAt)
		}
		reason, _ := testRdb.HGet(ctx, "agentlink:task:t-reopen-done", "reopen_reason").Result()
		if reason != "上次权限不足,现已开通" {
			t.Errorf("expected reopen_reason stored, got %s", reason)
		}

		// Re-added to tracking set
		members, _ := testRdb.SMembers(ctx, "agentlink:tasks:task-test:worker").Result()
		if !containsStr(members, "t-reopen-done") {
			t.Error("task should be back in tracking set after reopen")
		}

		// Inbox item: task type, content with reason prefix, original title preserved
		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		if data == "" {
			t.Fatal("expected inbox item after reopen")
		}
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Type != MsgTypeTask {
			t.Errorf("expected type=task, got %s", msg.Type)
		}
		if msg.TaskID != "t-reopen-done" {
			t.Errorf("expected task_id=t-reopen-done, got %s", msg.TaskID)
		}
		if msg.Title != "诊断bug" {
			t.Errorf("expected title=诊断bug, got %s", msg.Title)
		}
		expectedContent := "[重发原因: 上次权限不足,现已开通]\n原任务内容"
		if msg.Content != expectedContent {
			t.Errorf("expected content=%q, got %q", expectedContent, msg.Content)
		}
		if msg.FromDevice != "task-test" || msg.FromSession != "main" {
			t.Errorf("expected from=task-test:main, got %s:%s", msg.FromDevice, msg.FromSession)
		}
	})

	t.Run("reopen cancelled", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-reopen-cancelled",
			"task_id", "t-reopen-cancelled", "status", "cancelled",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "原任务", "title", "",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-reopen-cancelled", "agentlink:tasks:task-test:worker") })

		body := `{"task_id":"t-reopen-cancelled","reason":"条件已恢复"}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		st, _ := testRdb.HGet(ctx, "agentlink:task:t-reopen-cancelled", "status").Result()
		if st != "issued" {
			t.Errorf("expected status=issued, got %s", st)
		}

		data, _ := testRdb.LIndex(ctx, "agentlink:inbox:task-test:worker", 0).Result()
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Content != "[重发原因: 条件已恢复]\n原任务" {
			t.Errorf("unexpected content: %q", msg.Content)
		}
	})

	t.Run("reopen in_progress rejected", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-reopen-ip", "task_id", "t-reopen-ip", "status", "in_progress")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-reopen-ip") })

		body := `{"task_id":"t-reopen-ip","reason":"why"}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for in_progress reopen, got %d", resp.StatusCode)
		}
	})

	t.Run("reopen suspended rejected", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-reopen-susp", "task_id", "t-reopen-susp", "status", "suspended")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-reopen-susp") })

		body := `{"task_id":"t-reopen-susp","reason":"why"}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for suspended reopen, got %d", resp.StatusCode)
		}
	})

	t.Run("reopen not found", func(t *testing.T) {
		body := `{"task_id":"t-reopen-missing","reason":"why"}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("reopen empty reason rejected", func(t *testing.T) {
		cleanInbox()
		testRdb.HSet(ctx, "agentlink:task:t-reopen-no-r", "task_id", "t-reopen-no-r", "status", "completed")
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-reopen-no-r") })

		body := `{"task_id":"t-reopen-no-r","reason":""}`
		resp, err := doReq("POST", "/tasks/reopen", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for empty reason, got %d", resp.StatusCode)
		}
	})

	t.Run("reopen no auth", func(t *testing.T) {
		body := `{"task_id":"x","reason":"y"}`
		resp, err := http.Post(ts.URL+"/tasks/reopen", "application/json", strings.NewReader(body))
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
		testRdb.HSet(ctx, "agentlink:task:t-status-ok",
			"task_id", "t-status-ok", "status", "completed",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "fix bug", "result", "done",
			"issued_at", "2026-01-01T00:00:00Z",
			"completed_at", "2026-01-01T01:00:00Z",
		)
		t.Cleanup(func() { testRdb.Del(ctx, "agentlink:task:t-status-ok") })

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

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-life-1", "status").Result()
		if status != "in_progress" {
			t.Errorf("expected in_progress after pull, got %s", status)
		}

		body = `{"task_id":"t-life-1","status":"completed","result":"fixed"}`
		resp, err = doReq("POST", "/tasks/result", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ = testRdb.HGet(ctx, "agentlink:task:t-life-1", "status").Result()
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

		testRdb.Del(ctx, "agentlink:task:t-life-1")
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

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-life-2", "status").Result()
		if status != "suspended" {
			t.Errorf("expected suspended, got %s", status)
		}

		body = `{"task_id":"t-life-2","content":"new spec: add OAuth"}`
		resp, err = doReq("POST", "/tasks/resume", body)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		status, _ = testRdb.HGet(ctx, "agentlink:task:t-life-2", "status").Result()
		if status != "issued" {
			t.Errorf("expected issued after resume, got %s", status)
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

		status, _ = testRdb.HGet(ctx, "agentlink:task:t-life-2", "status").Result()
		if status != "completed" {
			t.Errorf("expected completed, got %s", status)
		}

		testRdb.Del(ctx, "agentlink:task:t-life-2")
		testRdb.Del(ctx, "agentlink:inbox:task-test:worker")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
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

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-life-3", "status").Result()
		if status != "cancelled" {
			t.Errorf("expected cancelled, got %s", status)
		}

		testRdb.Del(ctx, "agentlink:task:t-life-3")
	})

	t.Run("task list received and sent", func(t *testing.T) {
		cleanInbox()
		defer testRdb.Del(ctx, "agentlink:tasks:task-test:worker", "agentlink:issued:task-test:worker")

		// Received task: assigned to worker
		testRdb.HSet(ctx, "agentlink:task:t-list-rec",
			"task_id", "t-list-rec", "status", "issued",
			"assigned_to", "task-test:worker", "issued_by", "task-test:main",
			"content", "received task",
			"issued_at", "2026-01-01T00:00:00Z",
		)
		testRdb.Expire(ctx, "agentlink:task:t-list-rec", 7*24*3600)
		testRdb.SAdd(ctx, "agentlink:tasks:task-test:worker", "t-list-rec")
		defer testRdb.Del(ctx, "agentlink:task:t-list-rec")

		// Sent task: issued by worker
		testRdb.HSet(ctx, "agentlink:task:t-list-sent",
			"task_id", "t-list-sent", "status", "in_progress",
			"assigned_to", "task-test:main", "issued_by", "task-test:worker",
			"content", "sent task",
			"issued_at", "2026-01-01T00:01:00Z",
		)
		testRdb.Expire(ctx, "agentlink:task:t-list-sent", 7*24*3600)
		testRdb.SAdd(ctx, "agentlink:issued:task-test:worker", "t-list-sent")
		defer testRdb.Del(ctx, "agentlink:task:t-list-sent")

		resp, err := doReq("GET", "/tasks/list?session=worker", "")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		var listResp struct {
			Received []map[string]string `json:"received"`
			Sent     []map[string]string `json:"sent"`
		}
		json.NewDecoder(resp.Body).Decode(&listResp)

		if len(listResp.Received) != 1 {
			t.Errorf("expected 1 received task, got %d", len(listResp.Received))
		} else if listResp.Received[0]["task_id"] != "t-list-rec" {
			t.Errorf("expected received task_id=t-list-rec, got %s", listResp.Received[0]["task_id"])
		}

		if len(listResp.Sent) != 1 {
			t.Errorf("expected 1 sent task, got %d", len(listResp.Sent))
		} else if listResp.Sent[0]["task_id"] != "t-list-sent" {
			t.Errorf("expected sent task_id=t-list-sent, got %s", listResp.Sent[0]["task_id"])
		}
	})

	// ===================== Concurrent send (atomicity) =====================

	t.Run("concurrent send only one succeeds", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")

		const N = 10
		var wg sync.WaitGroup
		wg.Add(N)
		results := make([]int, N) // 1=200, 0=409, -1=other

		var mu sync.Mutex
		var firstErr string

		for i := 0; i < N; i++ {
			i := i
			go func() {
				defer wg.Done()
				body := fmt.Sprintf(`{"to":"task-test:worker","from_session":"main","task_id":"t-concurrent-%d","content":"race"}`, i)
				resp, err := doReq("POST", "/tasks/send", body)
				if err != nil {
					mu.Lock()
					if firstErr == "" {
						firstErr = err.Error()
					}
					mu.Unlock()
					results[i] = -1
					return
				}
				defer resp.Body.Close()
				switch resp.StatusCode {
				case http.StatusOK:
					results[i] = 1
				case http.StatusConflict:
					results[i] = 0
				default:
					results[i] = -1
				}
			}()
		}
		wg.Wait()

		if firstErr != "" {
			t.Fatalf("unexpected error: %s", firstErr)
		}

		successes := 0
		rejections := 0
		for _, r := range results {
			if r == 1 {
				successes++
			} else if r == 0 {
				rejections++
			} else {
				t.Errorf("unexpected status code in goroutine %d", r)
			}
		}
		if successes != 1 {
			t.Errorf("expected exactly 1 success, got %d (rejections=%d)", successes, rejections)
		}
		if rejections != N-1 {
			t.Errorf("expected %d rejections, got %d", N-1, rejections)
		}

		// Verify only one task record in Redis
		keys, _ := testRdb.Keys(ctx, "agentlink:task:t-concurrent-*").Result()
		if len(keys) != 1 {
			t.Errorf("expected 1 task record, got %d: %v", len(keys), keys)
		}

		// Cleanup
		for _, k := range keys {
			testRdb.Del(ctx, k)
		}
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	t.Run("busy on issued blocks new task", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")

		// First task succeeds (status=issued)
		body1 := `{"to":"task-test:worker","from_session":"main","task_id":"t-issued-1","content":"first"}`
		resp1, err := doReq("POST", "/tasks/send", body1)
		if err != nil {
			t.Fatal(err)
		}
		resp1.Body.Close()
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("first send expected 200, got %d", resp1.StatusCode)
		}

		// Second task should be rejected because first is still issued
		body2 := `{"to":"task-test:worker","from_session":"main","task_id":"t-issued-2","content":"second"}`
		resp2, err := doReq("POST", "/tasks/send", body2)
		if err != nil {
			t.Fatal(err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 for second task while first is issued, got %d", resp2.StatusCode)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:task:t-issued-1", "agentlink:task:t-issued-2")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	// ===================== Interrupt suspends in_progress task =====================

	t.Run("interrupt msg suspends in_progress task", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")

		// Send task and pull to make it in_progress
		body1 := `{"to":"task-test:worker","from_session":"main","task_id":"t-int-target","content":"long task"}`
		resp1, err := doReq("POST", "/tasks/send", body1)
		if err != nil {
			t.Fatal(err)
		}
		resp1.Body.Close()

		resp2, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		resp2.Body.Close()

		status, _ := testRdb.HGet(ctx, "agentlink:task:t-int-target", "status").Result()
		if status != "in_progress" {
			t.Fatalf("expected task in_progress after pull, got %s", status)
		}

		// Send interrupt msg
		body2 := `{"to":"task-test:worker","from_session":"main","interrupt":true,"content":"stop now"}`
		resp3, err := doReq("POST", "/messages/send", body2)
		if err != nil {
			t.Fatal(err)
		}
		defer resp3.Body.Close()
		if resp3.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for interrupt msg, got %d", resp3.StatusCode)
		}

		// Original task should be suspended
		status, _ = testRdb.HGet(ctx, "agentlink:task:t-int-target", "status").Result()
		if status != "suspended" {
			t.Errorf("expected original task suspended after interrupt, got %s", status)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:task:t-int-target")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	t.Run("interrupt task suspends in_progress task and enters inbox", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")

		// First task → in_progress
		body1 := `{"to":"task-test:worker","from_session":"main","task_id":"t-int-orig","content":"long task"}`
		resp1, err := doReq("POST", "/tasks/send", body1)
		if err != nil {
			t.Fatal(err)
		}
		resp1.Body.Close()

		resp2, err := doReq("GET", "/inbox/pull?session=worker&limit=1", "")
		if err != nil {
			t.Fatal(err)
		}
		resp2.Body.Close()

		// Interrupt task should succeed (not 409) and suspend original
		body2 := `{"to":"task-test:worker","from_session":"main","task_id":"t-int-urgent","interrupt":true,"content":"urgent fix"}`
		resp3, err := doReq("POST", "/tasks/send", body2)
		if err != nil {
			t.Fatal(err)
		}
		defer resp3.Body.Close()
		if resp3.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for interrupt task, got %d", resp3.StatusCode)
		}

		origStatus, _ := testRdb.HGet(ctx, "agentlink:task:t-int-orig", "status").Result()
		if origStatus != "suspended" {
			t.Errorf("expected original task suspended, got %s", origStatus)
		}

		urgentStatus, _ := testRdb.HGet(ctx, "agentlink:task:t-int-urgent", "status").Result()
		if urgentStatus != "issued" {
			t.Errorf("expected urgent task issued, got %s", urgentStatus)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:task:t-int-orig", "agentlink:task:t-int-urgent")
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")
	})

	t.Run("interrupt msg with no in_progress task succeeds", func(t *testing.T) {
		cleanInbox()
		testRdb.Del(ctx, "agentlink:tasks:task-test:worker")

		// No task in progress, interrupt msg should still work
		body := `{"to":"task-test:worker","from_session":"main","interrupt":true,"content":"urgent"}`
		resp, err := doReq("POST", "/messages/send", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// Cleanup
		testRdb.Del(ctx, "agentlink:inbox:task-test:worker")
	})

}

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func setupAgent(t *testing.T, device string, sessions []string, lastSeen time.Time) (apiKey string) {
	t.Helper()
	ctx := context.Background()

	sessionsJSON, _ := json.Marshal(sessions)
	apiKey = "sk_live_test_" + device
	h := sha256.Sum256([]byte(apiKey))
	hashHex := hex.EncodeToString(h[:])

	err := testRdb.HSet(ctx, "agentlink:device:"+device,
		"sessions", string(sessionsJSON),
		"api_key_hash", hashHex,
		"registered_at", lastSeen.Format(time.RFC3339),
		"last_seen", lastSeen.Format(time.RFC3339),
	).Err()
	if err != nil {
		t.Fatal(err)
	}
	testRdb.Set(ctx, "agentlink:api_key:"+hashHex, device, 0)

	return apiKey
}

func cleanupAgent(t *testing.T, device string) {
	t.Helper()
	ctx := context.Background()

	h := sha256.Sum256([]byte("sk_live_test_" + device))
	hashHex := hex.EncodeToString(h[:])
	testRdb.Del(ctx, "agentlink:device:"+device, "agentlink:api_key:"+hashHex)
}

func TestHeartbeat(t *testing.T) {
	now := time.Now()
	apiKey := setupAgent(t, "hb-device", []string{"main"}, now)
	defer cleanupAgent(t, "hb-device")

	// Set last_seen to an old value first
	ctx := context.Background()
	oldTime := now.Add(-5 * time.Minute).Format(time.RFC3339)
	testRdb.HSet(ctx, "agentlink:device:hb-device", "last_seen", oldTime)

	t.Run("heartbeat updates last_seen", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/agents/heartbeat", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Read response to ensure valid JSON
		var result map[string]bool
		json.NewDecoder(resp.Body).Decode(&result)
		if !result["ok"] {
			t.Error("expected ok=true")
		}

		// Verify last_seen was updated (should now be recent)
		lastSeen, _ := testRdb.HGet(ctx, "agentlink:device:hb-device", "last_seen").Result()
		t1, _ := time.Parse(time.RFC3339, lastSeen)
		t2, _ := time.Parse(time.RFC3339, oldTime)
		if !t1.After(t2) {
			t.Error("expected last_seen to be updated to a later time")
		}
	})

	t.Run("heartbeat no auth returns 401", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/agents/heartbeat", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestList(t *testing.T) {
	now := time.Now()

	// Create two devices: one freshly seen (online), one old (offline)
	onlineKey := setupAgent(t, "list-online", []string{"main", "worker"}, now)
	defer cleanupAgent(t, "list-online")

	oldTime := now.Add(-5 * time.Minute)
	setupAgent(t, "list-offline", []string{"reviewer"}, oldTime)
	defer cleanupAgent(t, "list-offline")

	t.Run("list current device only", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/agents/list", nil)
		req.Header.Set("Authorization", "Bearer "+onlineKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var lr ListResponse
		json.NewDecoder(resp.Body).Decode(&lr)
		if len(lr.Agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(lr.Agents))
		}
		if lr.Agents[0].Device != "list-online" {
			t.Errorf("expected device=list-online, got %s", lr.Agents[0].Device)
		}
		if len(lr.Agents[0].Sessions) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(lr.Agents[0].Sessions))
		}
		if !lr.Agents[0].Online {
			t.Error("expected online=true for recently seen device")
		}
	})

	t.Run("list all devices", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/agents/list?all=true", nil)
		req.Header.Set("Authorization", "Bearer "+onlineKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var lr ListResponse
		json.NewDecoder(resp.Body).Decode(&lr)
		if len(lr.Agents) < 2 {
			t.Fatalf("expected at least 2 agents, got %d", len(lr.Agents))
		}

		// Build a map for easy lookup
		agents := make(map[string]AgentInfo)
		for _, a := range lr.Agents {
			agents[a.Device] = a
		}

		online, ok := agents["list-online"]
		if !ok {
			t.Fatal("expected list-online in response")
		}
		if !online.Online {
			t.Error("expected list-online to be online")
		}

		offline, ok := agents["list-offline"]
		if !ok {
			t.Fatal("expected list-offline in response")
		}
		if offline.Online {
			t.Error("expected list-offline to be offline (last_seen > 120s ago)")
		}
		if len(offline.Sessions) != 1 || offline.Sessions[0] != "reviewer" {
			t.Errorf("expected sessions=[reviewer], got %v", offline.Sessions)
		}
	})

	t.Run("session_status current field", func(t *testing.T) {
		ctx := context.Background()
		// Device with: idle session, task session, msg session, offline session
		setupAgent(t, "list-status", []string{"idle-s", "task-s", "msg-s", "offline-s"}, now)
		defer cleanupAgent(t, "list-status")
		t.Cleanup(func() {
			testRdb.Del(ctx, "agentlink:task:t-status-task")
			testRdb.Del(ctx, "agentlink:tasks:list-status:task-s")
			testRdb.Del(ctx, "agentlink:current_msg:list-status:msg-s")
		})

		// task-s: in_progress task
		testRdb.HSet(ctx, "agentlink:task:t-status-task",
			"task_id", "t-status-task", "status", "in_progress",
			"title", "诊断bug", "issued_at", now.Format(time.RFC3339))
		testRdb.SAdd(ctx, "agentlink:tasks:list-status:task-s", "t-status-task")

		// msg-s: current_msg set
		testRdb.HSet(ctx, "agentlink:current_msg:list-status:msg-s",
			"title", "通知", "started_at", now.Format(time.RFC3339))
		testRdb.Expire(ctx, "agentlink:current_msg:list-status:msg-s", 10*time.Minute)

		// offline-s: no special state, but device is online (last_seen=now),
		// so offline-s will show "idle" not "offline". To test offline we
		// need a separate device with stale heartbeat — covered by list-offline.

		req, _ := http.NewRequest("GET", ts.URL+"/agents/list?all=true", nil)
		req.Header.Set("Authorization", "Bearer "+onlineKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var lr ListResponse
		json.NewDecoder(resp.Body).Decode(&lr)

		var target *AgentInfo
		for i := range lr.Agents {
			if lr.Agents[i].Device == "list-status" {
				target = &lr.Agents[i]
				break
			}
		}
		if target == nil {
			t.Fatal("list-status device not found in response")
		}

		currentBySession := map[string]string{}
		for _, ss := range target.SessionStatus {
			currentBySession[ss.Name] = ss.Current
		}

		if c := currentBySession["idle-s"]; c != "idle" {
			t.Errorf("idle-s: expected current=idle, got %q", c)
		}
		if c := currentBySession["task-s"]; !strings.HasPrefix(c, "task: t-status-task 诊断bug (") {
			t.Errorf("task-s: expected current to start with 'task: t-status-task 诊断bug (', got %q", c)
		}
		if c := currentBySession["msg-s"]; !strings.HasPrefix(c, "msg: 通知 (") {
			t.Errorf("msg-s: expected current to start with 'msg: 通知 (', got %q", c)
		}
		if c := currentBySession["offline-s"]; c != "idle" {
			t.Errorf("offline-s (device online, session idle): expected current=idle, got %q", c)
		}
	})

	t.Run("list no auth returns 401", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/agents/list")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestWhoami(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	apiKey := setupAgent(t, "whoami-test", []string{"main", "worker"}, now)
	defer cleanupAgent(t, "whoami-test")

	doReq := func(method, path string) (*http.Response, error) {
		req, _ := http.NewRequest(method, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultClient.Do(req)
	}

	// Setup a received task (in_progress so current shows "task:...")
	testRdb.HSet(ctx, "agentlink:task:t-whoami-rec",
		"task_id", "t-whoami-rec", "status", "in_progress",
		"assigned_to", "whoami-test:main", "issued_by", "whoami-test:worker",
		"title", "诊断bug", "content", "fix bug",
		"issued_at", now.Format(time.RFC3339),
	)
	testRdb.Expire(ctx, "agentlink:task:t-whoami-rec", 7*24*3600)
	testRdb.SAdd(ctx, "agentlink:tasks:whoami-test:main", "t-whoami-rec")
	defer testRdb.Del(ctx, "agentlink:task:t-whoami-rec", "agentlink:tasks:whoami-test:main")

	// Setup a sent task
	testRdb.HSet(ctx, "agentlink:task:t-whoami-sent",
		"task_id", "t-whoami-sent", "status", "issued",
		"assigned_to", "whoami-test:worker", "issued_by", "whoami-test:main",
		"title", "加监控", "content", "add monitoring",
		"issued_at", now.Format(time.RFC3339),
	)
	testRdb.Expire(ctx, "agentlink:task:t-whoami-sent", 7*24*3600)
	testRdb.SAdd(ctx, "agentlink:issued:whoami-test:main", "t-whoami-sent")
	defer testRdb.Del(ctx, "agentlink:task:t-whoami-sent", "agentlink:issued:whoami-test:main")

	resp, err := doReq("GET", "/whoami?session=main")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var w struct {
		Device        string `json:"device"`
		Session       string `json:"session"`
		Current       string `json:"current"`
		Inbox         map[string]int `json:"inbox"`
		ReceivedTasks []map[string]string `json:"received_tasks"`
		SentTasks     []map[string]string `json:"sent_tasks"`
	}
	json.NewDecoder(resp.Body).Decode(&w)

	if w.Device != "whoami-test" || w.Session != "main" {
		t.Errorf("expected whoami-test:main, got %s:%s", w.Device, w.Session)
	}

	// Should see 1 sent task (in_progress → current = "task: ...")
	if !strings.HasPrefix(w.Current, "task:") {
		t.Errorf("expected current=task:..., got %s", w.Current)
	}

	if len(w.ReceivedTasks) != 1 || w.ReceivedTasks[0]["task_id"] != "t-whoami-rec" {
		t.Errorf("expected 1 received task=t-whoami-rec, got %+v", w.ReceivedTasks)
	}
	if len(w.SentTasks) != 1 || w.SentTasks[0]["task_id"] != "t-whoami-sent" {
		t.Errorf("expected 1 sent task=t-whoami-sent, got %+v", w.SentTasks)
	}
}

func TestListOnlineDetection(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	apiKey := setupAgent(t, "online-test", []string{"main"}, now)
	defer cleanupAgent(t, "online-test")

	checkOnline := func(expected bool) {
		t.Helper()
		req, _ := http.NewRequest("GET", ts.URL+"/agents/list", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var lr ListResponse
		json.NewDecoder(resp.Body).Decode(&lr)
		if len(lr.Agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(lr.Agents))
		}
		if lr.Agents[0].Online != expected {
			t.Errorf("expected online=%v, got %v", expected, lr.Agents[0].Online)
		}
	}

	t.Run("recent last_seen is online", func(t *testing.T) {
		testRdb.HSet(ctx, "agentlink:device:online-test", "last_seen", now.Format(time.RFC3339))
		checkOnline(true)
	})

	t.Run("old last_seen is offline", func(t *testing.T) {
		testRdb.HSet(ctx, "agentlink:device:online-test", "last_seen", now.Add(-3*time.Minute).Format(time.RFC3339))
		checkOnline(false)
	})

	t.Run("exactly 119s is online", func(t *testing.T) {
		testRdb.HSet(ctx, "agentlink:device:online-test", "last_seen", now.Add(-119*time.Second).Format(time.RFC3339))
		checkOnline(true)
	})

	t.Run("exactly 121s is offline", func(t *testing.T) {
		testRdb.HSet(ctx, "agentlink:device:online-test", "last_seen", now.Add(-121*time.Second).Format(time.RFC3339))
		checkOnline(false)
	})
}

func TestListEmptySessions(t *testing.T) {
	now := time.Now()

	apiKey := setupAgent(t, "empty-sess", []string{}, now)
	defer cleanupAgent(t, "empty-sess")

	req, _ := http.NewRequest("GET", ts.URL+"/agents/list", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var lr ListResponse
	json.NewDecoder(resp.Body).Decode(&lr)
	if len(lr.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(lr.Agents))
	}
	if strings.Join(lr.Agents[0].Sessions, ",") != "" {
		t.Errorf("expected empty sessions, got %v", lr.Agents[0].Sessions)
	}
}

func TestPatchSessions(t *testing.T) {
	ctx := context.Background()
	apiKey := setupAgent(t, "patch-sess", []string{"main", "worker"}, time.Now())
	defer cleanupAgent(t, "patch-sess")

	doReq := func(sessions []string) (*http.Response, error) {
		body, _ := json.Marshal(map[string][]string{"sessions": sessions})
		req, _ := http.NewRequest("PATCH", ts.URL+"/agents/sessions", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultClient.Do(req)
	}

	t.Run("add session", func(t *testing.T) {
		resp, err := doReq([]string{"main", "worker", "reviewer"})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		sessionsJSON, _ := testRdb.HGet(ctx, "agentlink:device:patch-sess", "sessions").Result()
		var sessions []string
		json.Unmarshal([]byte(sessionsJSON), &sessions)
		if len(sessions) != 3 || sessions[2] != "reviewer" {
			t.Errorf("expected 3 sessions ending with reviewer, got %v", sessions)
		}
	})

	t.Run("remove session", func(t *testing.T) {
		resp, err := doReq([]string{"main", "worker"})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		sessionsJSON, _ := testRdb.HGet(ctx, "agentlink:device:patch-sess", "sessions").Result()
		var sessions []string
		json.Unmarshal([]byte(sessionsJSON), &sessions)
		if len(sessions) != 2 {
			t.Errorf("expected 2 sessions, got %v", sessions)
		}
	})

	t.Run("empty sessions", func(t *testing.T) {
		resp, err := doReq([]string{})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for empty sessions, got %d", resp.StatusCode)
		}
	})

	t.Run("no auth", func(t *testing.T) {
		body := `{"sessions":["main"]}`
		resp, err := http.Post(ts.URL+"/agents/sessions", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestDeleteSession(t *testing.T) {
	ctx := context.Background()
	apiKey := setupAgent(t, "del-sess", []string{"main", "worker", "reviewer"}, time.Now())
	defer cleanupAgent(t, "del-sess")

	// Put something in the inbox to verify cleanup
	inboxKey := "agentlink:inbox:del-sess:reviewer"
	testRdb.LPush(ctx, inboxKey, `{"id":"test"}`)

	doReq := func(name string) (*http.Response, error) {
		req, _ := http.NewRequest("DELETE", ts.URL+"/agents/sessions?name="+name, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return http.DefaultClient.Do(req)
	}

	t.Run("remove existing session", func(t *testing.T) {
		resp, err := doReq("reviewer")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Verify session removed from list
		sessionsJSON, _ := testRdb.HGet(ctx, "agentlink:device:del-sess", "sessions").Result()
		var sessions []string
		json.Unmarshal([]byte(sessionsJSON), &sessions)
		if len(sessions) != 2 {
			t.Errorf("expected 2 sessions, got %v", sessions)
		}

		// Verify inbox cleaned up
		exists, _ := testRdb.Exists(ctx, inboxKey).Result()
		if exists != 0 {
			t.Error("expected inbox to be deleted")
		}
	})

	t.Run("remove non-existing session", func(t *testing.T) {
		resp, err := doReq("nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("remove last session", func(t *testing.T) {
		resp, err := doReq("worker")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		resp, err = doReq("main")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for removing last session, got %d", resp.StatusCode)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", ts.URL+"/agents/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("no auth", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", ts.URL+"/agents/sessions?name=main", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestDeleteDevice(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	apiKey := setupAgent(t, "del-dev", []string{"main", "worker"}, now)
	h := sha256.Sum256([]byte(apiKey))
	hashHex := hex.EncodeToString(h[:])

	// Create inbox and task data to verify cleanup
	testRdb.LPush(ctx, "agentlink:inbox:del-dev:main", `{"id":"msg1"}`)
	testRdb.LPush(ctx, "agentlink:inbox:del-dev:worker", `{"id":"msg2"}`)
	testRdb.HSet(ctx, "agentlink:task:t-del-dev", "status", "issued", "assigned_to", "del-dev:worker")
	testRdb.SAdd(ctx, "agentlink:tasks:del-dev:worker", "t-del-dev")
	// Per-device:session keys that were leaking before issue 42: processing
	// (reserve-but-unacked), current_msg (status), issued (tasks handed out).
	testRdb.LPush(ctx, "agentlink:processing:del-dev:worker", `{"id":"msg2"}`)
	testRdb.Set(ctx, "agentlink:current_msg:del-dev:worker", "busy", 0)
	testRdb.SAdd(ctx, "agentlink:issued:del-dev:main", "t-del-dev")

	t.Run("delete device", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", ts.URL+"/agents/device", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Verify device deleted
		exists, _ := testRdb.Exists(ctx, "agentlink:device:del-dev").Result()
		if exists != 0 {
			t.Error("expected device:del-dev to be deleted")
		}

		// Verify API key index deleted
		exists, _ = testRdb.Exists(ctx, "agentlink:api_key:"+hashHex).Result()
		if exists != 0 {
			t.Error("expected api_key index to be deleted")
		}

		// Verify inboxes deleted
		exists, _ = testRdb.Exists(ctx, "agentlink:inbox:del-dev:main", "agentlink:inbox:del-dev:worker").Result()
		if exists != 0 {
			t.Error("expected inboxes to be deleted")
		}

		// Verify tasks and tracking sets deleted
		exists, _ = testRdb.Exists(ctx, "agentlink:task:t-del-dev").Result()
		if exists != 0 {
			t.Error("expected task to be deleted")
		}
		exists, _ = testRdb.Exists(ctx, "agentlink:tasks:del-dev:worker").Result()
		if exists != 0 {
			t.Error("expected tracking set to be deleted")
		}

		// Verify the previously-leaking per-device:session keys are gone (#42)
		exists, _ = testRdb.Exists(ctx,
			"agentlink:processing:del-dev:worker",
			"agentlink:current_msg:del-dev:worker",
			"agentlink:issued:del-dev:main",
		).Result()
		if exists != 0 {
			t.Errorf("expected processing/current_msg/issued to be deleted, %d still present", exists)
		}
	})

	t.Run("delete device no auth", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/agents/device", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}

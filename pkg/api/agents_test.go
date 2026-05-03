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

	err := testRdb.HSet(ctx, "device:"+device,
		"sessions", string(sessionsJSON),
		"api_key_hash", hashHex,
		"registered_at", lastSeen.Format(time.RFC3339),
		"last_seen", lastSeen.Format(time.RFC3339),
	).Err()
	if err != nil {
		t.Fatal(err)
	}
	testRdb.Set(ctx, "api_key:"+hashHex, device, 0)

	return apiKey
}

func cleanupAgent(t *testing.T, device string) {
	t.Helper()
	ctx := context.Background()

	h := sha256.Sum256([]byte("sk_live_test_" + device))
	hashHex := hex.EncodeToString(h[:])
	testRdb.Del(ctx, "device:"+device, "api_key:"+hashHex)
}

func TestHeartbeat(t *testing.T) {
	now := time.Now()
	apiKey := setupAgent(t, "hb-device", []string{"main"}, now)
	defer cleanupAgent(t, "hb-device")

	// Set last_seen to an old value first
	ctx := context.Background()
	oldTime := now.Add(-5 * time.Minute).Format(time.RFC3339)
	testRdb.HSet(ctx, "device:hb-device", "last_seen", oldTime)

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
		lastSeen, _ := testRdb.HGet(ctx, "device:hb-device", "last_seen").Result()
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
		testRdb.HSet(ctx, "device:online-test", "last_seen", now.Format(time.RFC3339))
		checkOnline(true)
	})

	t.Run("old last_seen is offline", func(t *testing.T) {
		testRdb.HSet(ctx, "device:online-test", "last_seen", now.Add(-3*time.Minute).Format(time.RFC3339))
		checkOnline(false)
	})

	t.Run("exactly 119s is online", func(t *testing.T) {
		testRdb.HSet(ctx, "device:online-test", "last_seen", now.Add(-119*time.Second).Format(time.RFC3339))
		checkOnline(true)
	})

	t.Run("exactly 121s is offline", func(t *testing.T) {
		testRdb.HSet(ctx, "device:online-test", "last_seen", now.Add(-121*time.Second).Format(time.RFC3339))
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

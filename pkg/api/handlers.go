package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var deviceNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

type HealthResponse struct {
	Ok    bool   `json:"ok"`
	Redis string `json:"redis"`
}

type RegisterRequest struct {
	Device           string   `json:"device"`
	Sessions         []string `json:"sessions"`
	RegisterPassword string   `json:"register_password"`
}

type RegisterResponse struct {
	APIKey       string   `json:"api_key"`
	Device       string   `json:"device"`
	Sessions     []string `json:"sessions"`
	RegisteredAt string   `json:"registered_at"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	redisStatus := "connected"
	if err := s.rdb.Ping(r.Context()).Err(); err != nil {
		redisStatus = "disconnected"
	}

	ok := redisStatus == "connected"
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, HealthResponse{
		Ok:    ok,
		Redis: redisStatus,
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate register password
	if req.RegisterPassword != s.registerPassword {
		writeError(w, http.StatusUnauthorized, "invalid register password")
		return
	}

	// Validate device name
	if !deviceNameRE.MatchString(req.Device) {
		writeError(w, http.StatusBadRequest, "invalid device name: 2-32 chars, lowercase letters/digits/hyphens/underscores, start with a letter")
		return
	}

	// Validate sessions
	if len(req.Sessions) == 0 {
		writeError(w, http.StatusBadRequest, "sessions must not be empty")
		return
	}
	for _, s := range req.Sessions {
		if !deviceNameRE.MatchString(s) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid session name %q: 2-32 chars, lowercase letters/digits/hyphens/underscores, start with a letter", s))
			return
		}
	}

	// Check device already exists
	exists, err := s.rdb.Exists(r.Context(), "device:"+req.Device).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exists > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("device %q already registered", req.Device))
		return
	}

	// Generate API key
	apiKey, apiKeyHash, err := generateAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Store device data in a Hash
	deviceKey := "device:" + req.Device
	sessionsJSON, _ := json.Marshal(req.Sessions)
	if err := s.rdb.HSet(r.Context(), deviceKey,
		"sessions", string(sessionsJSON),
		"api_key_hash", apiKeyHash,
		"registered_at", now,
		"last_seen", now,
	).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Reverse index for auth lookup
	indexKey := "api_key:" + apiKeyHash
	if err := s.rdb.Set(r.Context(), indexKey, req.Device, 0).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, RegisterResponse{
		APIKey:       apiKey,
		Device:       req.Device,
		Sessions:     req.Sessions,
		RegisteredAt: now,
	})
}

type Message struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	FromDevice  string `json:"from_device"`
	FromSession string `json:"from_session"`
	TaskID      string `json:"task_id,omitempty"`
	Content     string `json:"content"`
	CreatedAt   string `json:"created_at"`
}

type SendRequest struct {
	To          string `json:"to"`
	FromSession string `json:"from_session"`
	Content     string `json:"content"`
}

type SendResponse struct {
	ID string `json:"id"`
}

type PullResponse struct {
	Items []Message `json:"items"`
}

type SendTaskRequest struct {
	To          string `json:"to"`
	FromSession string `json:"from_session"`
	TaskID      string `json:"task_id"`
	Content     string `json:"content"`
}

type TaskResultRequest struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Result string `json:"result"`
}

type TaskResumeRequest struct {
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
}

type TaskCancelRequest struct {
	TaskID string `json:"task_id"`
}

type TaskStatusResponse struct {
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	AssignedTo  string `json:"assigned_to"`
	IssuedBy    string `json:"issued_by"`
	Content     string `json:"content"`
	Result      string `json:"result,omitempty"`
	IssuedAt    string `json:"issued_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type AgentInfo struct {
	Device    string   `json:"device"`
	Sessions  []string `json:"sessions"`
	LastSeen  string   `json:"last_seen"`
	Online    bool     `json:"online"`
}

type ListResponse struct {
	Agents []AgentInfo `json:"agents"`
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.rdb.HSet(r.Context(), "device:"+device, "last_seen", now).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)
	all := r.URL.Query().Get("all") == "true"

	var agents []AgentInfo

	if all {
		keys, err := s.rdb.Keys(r.Context(), "device:*").Result()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, key := range keys {
			name := strings.TrimPrefix(key, "device:")
			agents = append(agents, s.getAgentInfo(r.Context(), name))
		}
	} else {
		agents = append(agents, s.getAgentInfo(r.Context(), device))
	}

	if agents == nil {
		agents = []AgentInfo{}
	}

	writeJSON(w, http.StatusOK, ListResponse{Agents: agents})
}

func (s *Server) getAgentInfo(ctx context.Context, device string) AgentInfo {
	data, err := s.rdb.HGetAll(ctx, "device:"+device).Result()
	if err != nil || len(data) == 0 {
		return AgentInfo{Device: device, Online: false}
	}

	var sessions []string
	json.Unmarshal([]byte(data["sessions"]), &sessions)

	lastSeen := data["last_seen"]
	online := false
	if lastSeen != "" {
		t, err := time.Parse(time.RFC3339, lastSeen)
		if err == nil {
			online = time.Since(t) < 120*time.Second
		}
	}

	return AgentInfo{
		Device:   device,
		Sessions: sessions,
		LastSeen: lastSeen,
		Online:   online,
	}
}

type PatchSessionsRequest struct {
	Sessions []string `json:"sessions"`
}

func (s *Server) handlePatchSessions(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)

	var req PatchSessionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Sessions) == 0 {
		writeError(w, http.StatusBadRequest, "sessions must not be empty")
		return
	}

	for _, session := range req.Sessions {
		if !deviceNameRE.MatchString(session) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid session name %q", session))
			return
		}
	}

	sessionsJSON, _ := json.Marshal(req.Sessions)
	if err := s.rdb.HSet(r.Context(), "device:"+device, "sessions", string(sessionsJSON)).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": req.Sessions})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)

	session := r.URL.Query().Get("name")
	if session == "" {
		writeError(w, http.StatusBadRequest, "missing name parameter")
		return
	}
	if !deviceNameRE.MatchString(session) {
		writeError(w, http.StatusBadRequest, "invalid session name")
		return
	}

	deviceKey := "device:" + device
	sessionsJSON, err := s.rdb.HGet(r.Context(), deviceKey, "sessions").Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var sessions []string
	json.Unmarshal([]byte(sessionsJSON), &sessions)

	found := false
	for i, s := range sessions {
		if s == session {
			sessions = append(sessions[:i], sessions[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	if len(sessions) == 0 {
		writeError(w, http.StatusBadRequest, "cannot remove the last session")
		return
	}

	updated, _ := json.Marshal(sessions)
	if err := s.rdb.HSet(r.Context(), deviceKey, "sessions", string(updated)).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Clean up inbox
	s.rdb.Del(r.Context(), "inbox:"+device+":"+session)

	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)
	ctx := r.Context()

	// Get API key hash to delete reverse index
	apiKeyHash, _ := s.rdb.HGet(ctx, "device:"+device, "api_key_hash").Result()

	// Delete device hash
	s.rdb.Del(ctx, "device:"+device)

	// Delete API key index
	if apiKeyHash != "" {
		s.rdb.Del(ctx, "api_key:"+apiKeyHash)
	}

	// Delete all inboxes for this device
	inboxKeys, _ := s.rdb.Keys(ctx, "inbox:"+device+":*").Result()
	for _, k := range inboxKeys {
		s.rdb.Del(ctx, k)
	}

	// Delete all task tracking sets and their tasks
	trackingKeys, _ := s.rdb.Keys(ctx, "tasks:"+device+":*").Result()
	for _, tk := range trackingKeys {
		members, _ := s.rdb.SMembers(ctx, tk).Result()
		for _, tid := range members {
			s.rdb.Del(ctx, "task:"+tid)
		}
		s.rdb.Del(ctx, tk)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	fromDevice, _ := r.Context().Value(contextKeyDevice).(string)

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.To == "" {
		writeError(w, http.StatusBadRequest, "missing field: to")
		return
	}
	if req.FromSession == "" {
		writeError(w, http.StatusBadRequest, "missing field: from_session")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "missing field: content")
		return
	}
	if len(req.Content) > 3000 {
		writeError(w, http.StatusBadRequest, "content exceeds 3000 characters")
		return
	}
	if !deviceNameRE.MatchString(req.FromSession) {
		writeError(w, http.StatusBadRequest, "invalid from_session name")
		return
	}

	parts := strings.SplitN(req.To, ":", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid target format, expected device:session")
		return
	}
	targetDevice, targetSession := parts[0], parts[1]
	if !deviceNameRE.MatchString(targetDevice) {
		writeError(w, http.StatusBadRequest, "invalid target device name")
		return
	}
	if !deviceNameRE.MatchString(targetSession) {
		writeError(w, http.StatusBadRequest, "invalid target session name")
		return
	}

	exists, err := s.rdb.Exists(r.Context(), "device:"+targetDevice).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exists == 0 {
		writeError(w, http.StatusNotFound, "target device not found")
		return
	}

	sessionsJSON, err := s.rdb.HGet(r.Context(), "device:"+targetDevice, "sessions").Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var sessions []string
	json.Unmarshal([]byte(sessionsJSON), &sessions)
	if !containsStr(sessions, targetSession) {
		writeError(w, http.StatusNotFound, "target session not found on device")
		return
	}

	id := generateID()
	now := time.Now().UTC().Format(time.RFC3339)
	msg := Message{
		ID:          id,
		Type:        "msg",
		FromDevice:  fromDevice,
		FromSession: req.FromSession,
		Content:     req.Content,
		CreatedAt:   now,
	}
	msgJSON, _ := json.Marshal(msg)

	inboxKey := "inbox:" + targetDevice + ":" + targetSession
	if err := s.rdb.LPush(r.Context(), inboxKey, msgJSON).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, SendResponse{ID: id})
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	device, _ := r.Context().Value(contextKeyDevice).(string)

	session := r.URL.Query().Get("session")
	if session == "" {
		writeError(w, http.StatusBadRequest, "missing session parameter")
		return
	}
	if !deviceNameRE.MatchString(session) {
		writeError(w, http.StatusBadRequest, "invalid session name")
		return
	}

	limit := 1
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n < 0 {
			writeError(w, http.StatusBadRequest, "limit must not be negative")
			return
		}
		if n > 0 {
			limit = n
		}
		if limit > 100 {
			limit = 100
		}
	}

	inboxKey := "inbox:" + device + ":" + session
	items := make([]Message, 0)

	for i := 0; i < limit; i++ {
		data, err := s.rdb.RPop(r.Context(), inboxKey).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		var msg Message
		json.Unmarshal([]byte(data), &msg)
		// Auto-set task to in_progress on pull
		if msg.Type == "task" && msg.TaskID != "" {
			taskKey := "task:" + msg.TaskID
			currentStatus, _ := s.rdb.HGet(r.Context(), taskKey, "status").Result()
			if currentStatus == "issued" {
				s.rdb.HSet(r.Context(), taskKey, "status", "in_progress")
			}
		}
		items = append(items, msg)
	}

	writeJSON(w, http.StatusOK, PullResponse{Items: items})
}

func (s *Server) handleSendTask(w http.ResponseWriter, r *http.Request) {
	fromDevice, _ := r.Context().Value(contextKeyDevice).(string)

	var req SendTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.To == "" {
		writeError(w, http.StatusBadRequest, "missing field: to")
		return
	}
	if req.FromSession == "" {
		writeError(w, http.StatusBadRequest, "missing field: from_session")
		return
	}
	if req.TaskID == "" {
		writeError(w, http.StatusBadRequest, "missing field: task_id")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "missing field: content")
		return
	}
	if len(req.Content) > 3000 {
		writeError(w, http.StatusBadRequest, "content exceeds 3000 characters")
		return
	}
	if !deviceNameRE.MatchString(req.TaskID) {
		writeError(w, http.StatusBadRequest, "invalid task_id: 2-32 chars, lowercase letters/digits/hyphens/underscores, start with a letter")
		return
	}
	if !deviceNameRE.MatchString(req.FromSession) {
		writeError(w, http.StatusBadRequest, "invalid from_session name")
		return
	}

	parts := strings.SplitN(req.To, ":", 2)
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "invalid target format, expected device:session")
		return
	}
	targetDevice, targetSession := parts[0], parts[1]
	if !deviceNameRE.MatchString(targetDevice) {
		writeError(w, http.StatusBadRequest, "invalid target device name")
		return
	}
	if !deviceNameRE.MatchString(targetSession) {
		writeError(w, http.StatusBadRequest, "invalid target session name")
		return
	}

	// Check target device exists
	exists, err := s.rdb.Exists(r.Context(), "device:"+targetDevice).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exists == 0 {
		writeError(w, http.StatusNotFound, "target device not found")
		return
	}

	// Check target session exists
	sessionsJSON, err := s.rdb.HGet(r.Context(), "device:"+targetDevice, "sessions").Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var sessions []string
	json.Unmarshal([]byte(sessionsJSON), &sessions)
	if !containsStr(sessions, targetSession) {
		writeError(w, http.StatusNotFound, "target session not found on device")
		return
	}

	// Check task_id uniqueness
	taskKey := "task:" + req.TaskID
	exists, err = s.rdb.Exists(r.Context(), taskKey).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if exists > 0 {
		writeError(w, http.StatusConflict, "task_id already exists")
		return
	}

	// Busy check: iterate tasks:<device>:<session> set
	trackingKey := "tasks:" + targetDevice + ":" + targetSession
	members, err := s.rdb.SMembers(r.Context(), trackingKey).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var inProgress bool
	suspendedCount := 0
	for _, tid := range members {
		status, err := s.rdb.HGet(r.Context(), "task:"+tid, "status").Result()
		if err != nil {
			continue
		}
		switch status {
		case "in_progress":
			inProgress = true
		case "suspended":
			suspendedCount++
		}
	}
	if inProgress {
		writeError(w, http.StatusConflict, "target has an in_progress task")
		return
	}
	if suspendedCount >= 2 {
		writeError(w, http.StatusConflict, "target has 2 suspended tasks")
		return
	}

	// Create task record
	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.rdb.HSet(r.Context(), taskKey,
		"task_id", req.TaskID,
		"status", "issued",
		"assigned_to", req.To,
		"issued_by", fromDevice+":"+req.FromSession,
		"content", req.Content,
		"issued_at", now,
	).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.rdb.Expire(r.Context(), taskKey, 7*24*time.Hour)

	// Add to tracking set
	s.rdb.SAdd(r.Context(), trackingKey, req.TaskID)

	// Push to target inbox
	id := generateID()
	inboxItem := Message{
		ID:          id,
		Type:        "task",
		FromDevice:  fromDevice,
		FromSession: req.FromSession,
		TaskID:      req.TaskID,
		Content:     req.Content,
		CreatedAt:   now,
	}
	msgJSON, _ := json.Marshal(inboxItem)
	inboxKey := "inbox:" + targetDevice + ":" + targetSession
	if err := s.rdb.LPush(r.Context(), inboxKey, msgJSON).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, SendResponse{ID: id})
}

func (s *Server) handleTaskResult(w http.ResponseWriter, r *http.Request) {
	var req TaskResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TaskID == "" {
		writeError(w, http.StatusBadRequest, "missing field: task_id")
		return
	}
	if req.Status != "completed" && req.Status != "suspended" {
		writeError(w, http.StatusBadRequest, "status must be completed or suspended")
		return
	}
	if req.Result == "" {
		writeError(w, http.StatusBadRequest, "missing field: result")
		return
	}

	taskKey := "task:" + req.TaskID
	status, err := s.rdb.HGet(r.Context(), taskKey, "status").Result()
	if err == redis.Nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if status != "in_progress" {
		writeError(w, http.StatusBadRequest, "task is not in_progress")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.rdb.HSet(r.Context(), taskKey,
		"status", req.Status,
		"result", req.Result,
		"completed_at", now,
	).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Remove from tracking set
	assignedTo, err := s.rdb.HGet(r.Context(), taskKey, "assigned_to").Result()
	if err == nil {
		parts := strings.SplitN(assignedTo, ":", 2)
		if len(parts) == 2 {
			s.rdb.SRem(r.Context(), "tasks:"+parts[0]+":"+parts[1], req.TaskID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTaskResume(w http.ResponseWriter, r *http.Request) {
	var req TaskResumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TaskID == "" {
		writeError(w, http.StatusBadRequest, "missing field: task_id")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "missing field: content")
		return
	}
	if len(req.Content) > 3000 {
		writeError(w, http.StatusBadRequest, "content exceeds 3000 characters")
		return
	}

	taskKey := "task:" + req.TaskID
	status, err := s.rdb.HGet(r.Context(), taskKey, "status").Result()
	if err == redis.Nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if status != "suspended" {
		writeError(w, http.StatusBadRequest, "task is not suspended")
		return
	}

	// Get task info before modifying
	taskData, err := s.rdb.HGetAll(r.Context(), taskKey).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.rdb.HSet(r.Context(), taskKey,
		"status", "in_progress",
		"content", req.Content,
		"issued_at", now, // Update issued_at to reflect new guidance
		"result", "",
		"completed_at", "",
	).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Re-add to tracking set
	assignedTo := taskData["assigned_to"]
	parts := strings.SplitN(assignedTo, ":", 2)
	if len(parts) == 2 {
		s.rdb.SAdd(r.Context(), "tasks:"+parts[0]+":"+parts[1], req.TaskID)
	}

	// Re-push to target inbox
	id := generateID()
	inboxItem := Message{
		ID:          id,
		Type:        "task",
		FromDevice:  taskData["issued_by"],
		FromSession: "",
		TaskID:      req.TaskID,
		Content:     req.Content,
		CreatedAt:   now,
	}
	// Extract from_device from issued_by (format: "device:session")
	issuedParts := strings.SplitN(taskData["issued_by"], ":", 2)
	if len(issuedParts) == 2 {
		inboxItem.FromDevice = issuedParts[0]
		inboxItem.FromSession = issuedParts[1]
	}

	msgJSON, _ := json.Marshal(inboxItem)
	if len(parts) == 2 {
		inboxKey := "inbox:" + parts[0] + ":" + parts[1]
		s.rdb.LPush(r.Context(), inboxKey, msgJSON)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	var req TaskCancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.TaskID == "" {
		writeError(w, http.StatusBadRequest, "missing field: task_id")
		return
	}

	taskKey := "task:" + req.TaskID
	status, err := s.rdb.HGet(r.Context(), taskKey, "status").Result()
	if err == redis.Nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if status == "completed" || status == "cancelled" {
		writeError(w, http.StatusBadRequest, "task already "+status)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.rdb.HSet(r.Context(), taskKey,
		"status", "cancelled",
		"completed_at", now,
	).Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Remove from tracking set
	assignedTo, err := s.rdb.HGet(r.Context(), taskKey, "assigned_to").Result()
	if err == nil {
		parts := strings.SplitN(assignedTo, ":", 2)
		if len(parts) == 2 {
			s.rdb.SRem(r.Context(), "tasks:"+parts[0]+":"+parts[1], req.TaskID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("task_id")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing task_id parameter")
		return
	}

	taskKey := "task:" + taskID
	data, err := s.rdb.HGetAll(r.Context(), taskKey).Result()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	resp := TaskStatusResponse{
		TaskID:      data["task_id"],
		Status:      data["status"],
		AssignedTo:  data["assigned_to"],
		IssuedBy:    data["issued_by"],
		Content:     data["content"],
		Result:      data["result"],
		IssuedAt:    data["issued_at"],
		CompletedAt: data["completed_at"],
	}
	writeJSON(w, http.StatusOK, resp)
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func generateAPIKey() (raw, sha256hex string, err error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", "", e
	}
	raw = "sk_live_" + hex.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(h[:]), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// skipAuth returns true if the path does not require API key auth.
func skipAuth(path string) bool {
	return path == "/health" || path == "/agents/register"
}

// authMiddleware wraps a handler, checking Bearer API key on protected routes.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || len(auth) < 8 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		apiKey := strings.TrimSpace(auth[7:])
		if apiKey == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		h := sha256.Sum256([]byte(apiKey))
		hashHex := hex.EncodeToString(h[:])
		device, err := s.rdb.Get(r.Context(), "api_key:"+hashHex).Result()
		if err == redis.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		// Inject device name into context
		ctx := context.WithValue(r.Context(), contextKeyDevice, device)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

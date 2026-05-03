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
		items = append(items, msg)
	}

	writeJSON(w, http.StatusOK, PullResponse{Items: items})
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

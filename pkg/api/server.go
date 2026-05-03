package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/team/agentlink/pkg/redis"
)

type contextKey string

const (
	contextKeyDevice contextKey = "device"
)

type Server struct {
	rdb              *redis.Client
	registerPassword string
	mux              *http.ServeMux
	srv              *http.Server
}

func New(addr string, rdb *redis.Client, registerPassword string) *Server {
	s := &Server{
		rdb:              rdb,
		registerPassword: registerPassword,
		mux:              http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /agents/register", s.handleRegister)
	s.mux.HandleFunc("POST /messages/send", s.handleSend)
	s.mux.HandleFunc("GET /inbox/pull", s.handlePull)
	s.mux.HandleFunc("POST /tasks/send", s.handleSendTask)
	s.mux.HandleFunc("POST /tasks/result", s.handleTaskResult)
	s.mux.HandleFunc("POST /tasks/resume", s.handleTaskResume)
	s.mux.HandleFunc("POST /tasks/cancel", s.handleTaskCancel)
	s.mux.HandleFunc("GET /tasks/status", s.handleTaskStatus)
	s.mux.HandleFunc("POST /agents/heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("GET /agents/list", s.handleList)

	return s
}

func (s *Server) ListenAndServe(addr string) error {
	s.srv = &http.Server{
		Addr:    addr,
		Handler: s.authMiddleware(s.mux),
	}
	fmt.Printf("API server listening on %s\n", addr)
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

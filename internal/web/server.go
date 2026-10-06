// Package web holds the HTTP server: mux, middleware and handlers.
package web

import (
	"database/sql"
	"net/http"
)

type Server struct {
	db  *sql.DB
	mux *http.ServeMux
}

func NewServer(db *sql.DB) *Server {
	s := &Server{db: db, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// handleHealthz reports liveness, including that the database answers.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

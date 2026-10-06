// Package web holds the HTTP server: mux, middleware and handlers.
package web

import (
	"database/sql"
	"html/template"
	"net/http"

	"musiclub/internal/store"
	"musiclub/static"
)

type Server struct {
	db       *sql.DB
	clubs    *store.Clubs
	players  *store.Players
	sessions sessions
	views    map[string]*template.Template
	mux      *http.ServeMux
}

func NewServer(db *sql.DB, cookieSecret string) *Server {
	s := &Server{
		db:       db,
		clubs:    store.NewClubs(db),
		players:  store.NewPlayers(db),
		sessions: sessions{secret: []byte(cookieSecret)},
		views:    parseViews(),
		mux:      http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	s.mux.HandleFunc("GET /{$}", s.handleIndex)

	s.mux.Handle("GET /c/{club}", s.clubRoute(s.handleClubHome))
	s.mux.Handle("GET /c/{club}/whoami", s.clubRoute(s.handleWhoami))
	s.mux.Handle("POST /c/{club}/whoami", s.clubRoute(s.handleSetWhoami))

	s.mux.Handle("GET /c/{club}/admin", s.adminRoute(s.handleAdmin))
	s.mux.Handle("POST /c/{club}/admin/players", s.adminRoute(s.handleAdminPlayers))
	return s
}

// Handler returns the server's root handler. State-changing requests from other
// origins are rejected (Sec-Fetch-Site / Origin check).
func (s *Server) Handler() http.Handler {
	return http.NewCrossOriginProtection().Handler(s.mux)
}

// handleHealthz reports liveness, including that the database answers.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

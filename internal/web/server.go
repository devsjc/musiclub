// Package web holds the HTTP server: mux, middleware and handlers.
package web

import (
	"database/sql"
	"html/template"
	"net/http"
	"time"

	"musiclub/internal/rounds"
	"musiclub/internal/store"
	"musiclub/static"
)

type Server struct {
	db          *sql.DB
	clubs       *store.Clubs
	players     *store.Players
	rounds      *store.Rounds
	submissions *store.Submissions
	roundSvc    *rounds.Service
	loc         *time.Location // zone for showing deadlines and choosing the default month
	sessions    sessions
	views       map[string]*template.Template
	mux         *http.ServeMux
}

func NewServer(db *sql.DB, cookieSecret string, loc *time.Location) *Server {
	s := &Server{
		db:          db,
		clubs:       store.NewClubs(db),
		players:     store.NewPlayers(db),
		rounds:      store.NewRounds(db),
		submissions: store.NewSubmissions(db),
		roundSvc:    rounds.New(db),
		loc:         loc,
		sessions:    sessions{secret: []byte(cookieSecret)},
		views:       parseViews(),
		mux:         http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))
	s.mux.HandleFunc("GET /{$}", s.handleIndex)

	s.mux.Handle("GET /c/{club}", s.clubRoute(s.handleClubHome))
	s.mux.Handle("GET /c/{club}/whoami", s.clubRoute(s.handleWhoami))
	s.mux.Handle("POST /c/{club}/whoami", s.clubRoute(s.handleSetWhoami))

	s.mux.Handle("GET /c/{club}/rounds/{month}", s.clubRoute(s.handleRound))
	s.mux.Handle("POST /c/{club}/rounds/{month}/theme", s.clubRoute(s.handleSetTheme))
	s.mux.Handle("POST /c/{club}/rounds/{month}/submission", s.clubRoute(s.handleSubmission))

	s.mux.Handle("GET /c/{club}/admin", s.adminRoute(s.handleAdmin))
	s.mux.Handle("POST /c/{club}/admin/players", s.adminRoute(s.handleAdminPlayers))
	s.mux.Handle("POST /c/{club}/admin/rounds", s.adminRoute(s.handleAdminStartRound))
	s.mux.Handle("POST /c/{club}/admin/rounds/{month}/reroll", s.adminRoute(s.handleAdminReroll))
	s.mux.Handle("POST /c/{club}/admin/rounds/{month}/submissions/{id}/delete", s.adminRoute(s.handleAdminDeleteSubmission))
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

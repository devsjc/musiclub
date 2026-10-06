package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"musiclub/internal/domain"
	"musiclub/internal/store"
)

type ctxKey int

const (
	clubKey ctxKey = iota
	playerKey
)

func clubFrom(ctx context.Context) domain.Club {
	c, _ := ctx.Value(clubKey).(domain.Club)
	return c
}

// playerFrom returns the current player, or nil if nobody has said who they are.
func playerFrom(ctx context.Context) *domain.Player {
	p, _ := ctx.Value(playerKey).(*domain.Player)
	return p
}

// clubRoute wraps a handler for a /c/{club}/... route: it resolves the club and
// the current player first.
func (s *Server) clubRoute(h http.HandlerFunc) http.Handler {
	return s.withClub(s.withPlayer(h))
}

// adminRoute is clubRoute plus a requirement that the current player is an admin.
func (s *Server) adminRoute(h http.HandlerFunc) http.Handler {
	return s.withClub(s.withPlayer(s.requireAdmin(h)))
}

// withClub resolves {club} to a Club once per request (DESIGN.md §5).
func (s *Server) withClub(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		club, err := s.clubs.BySlug(r.Context(), r.PathValue("club"))
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, r, http.StatusNotFound, "No such club.")
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		if err := s.roundSvc.Tick(r.Context(), time.Now()); err != nil {
			slog.Error("lazy tick failed", "err", err)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clubKey, club)))
	})
}

// withPlayer reads the signed cookie and, if it names an active player in this
// club, puts them on the context. A missing, forged or stale cookie just means
// "nobody"; it is never an error.
func (s *Server) withPlayer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		club := clubFrom(r.Context())
		if c, err := r.Cookie(cookieName(club.Slug)); err == nil {
			if id, ok := s.sessions.verify(club.Slug, c.Value); ok {
				p, err := s.players.Get(r.Context(), club.ID, id)
				switch {
				case err == nil && p.Active:
					r = r.WithContext(context.WithValue(r.Context(), playerKey, &p))
				case err == nil || errors.Is(err, store.ErrNotFound):
				default:
					s.serverError(w, r, err)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin sends anonymous visitors to whoami and rejects non-admins.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := playerFrom(r.Context())
		if p == nil {
			http.Redirect(w, r, clubPath(r.Context(), "/whoami"), http.StatusSeeOther)
			return
		}
		if !p.IsAdmin {
			s.fail(w, r, http.StatusForbidden, "Admins only.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clubPath(ctx context.Context, suffix string) string {
	return "/c/" + clubFrom(ctx).Slug + suffix
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	s.fail(w, r, http.StatusInternalServerError, "Something went wrong.")
}

package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"musiclub/internal/domain"
	"musiclub/internal/store"
)

const cookieMaxAge = 365 * 24 * time.Hour

func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	s.renderWhoami(w, r, http.StatusOK, "")
}

func (s *Server) renderWhoami(w http.ResponseWriter, r *http.Request, status int, msg string) {
	all, err := s.players.List(r.Context(), clubFrom(r.Context()).ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var active []domain.Player
	for _, p := range all {
		if p.Active {
			active = append(active, p)
		}
	}
	s.render(w, r, status, "whoami", page{Title: "Who are you?", Error: msg, Data: active})
}

// handleSetWhoami stores the chosen player in the club's signed cookie.
func (s *Server) handleSetWhoami(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())

	id, err := strconv.ParseInt(r.FormValue("player_id"), 10, 64)
	if err != nil {
		s.renderWhoami(w, r, http.StatusBadRequest, "Choose a player.")
		return
	}
	p, err := s.players.Get(r.Context(), club.ID, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.Active) {
		s.renderWhoami(w, r, http.StatusBadRequest, "Choose a player from the list.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(club.Slug),
		Value:    s.sessions.sign(club.Slug, p.ID),
		Path:     "/c/" + club.Slug,
		MaxAge:   int(cookieMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/c/"+club.Slug, http.StatusSeeOther)
}

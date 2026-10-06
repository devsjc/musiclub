package web

import (
	"errors"
	"net/http"

	"musiclub/internal/store"
)

// handleIndex lists the clubs, or goes straight to the club if there is only one.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	clubs, err := s.clubs.List(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(clubs) == 1 {
		http.Redirect(w, r, "/c/"+clubs[0].Slug, http.StatusFound)
		return
	}
	s.render(w, r, http.StatusOK, "index", page{Title: "Clubs", Data: clubs})
}

// handleClubHome is the club landing page: the current round, if there is one.
func (s *Server) handleClubHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var current *roundView
	round, err := s.rounds.Current(ctx, clubFrom(ctx).ID)
	switch {
	case err == nil:
		v, err := s.roundView(ctx, round)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		current = &v
	case !errors.Is(err, store.ErrNotFound):
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "home", page{Title: clubFrom(ctx).Name, Data: current})
}

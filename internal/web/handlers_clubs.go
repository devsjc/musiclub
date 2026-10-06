package web

import (
	"net/http"
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

// handleClubHome is the club landing page. Rounds arrive in a later stage.
func (s *Server) handleClubHome(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", page{Title: clubFrom(r.Context()).Name})
}

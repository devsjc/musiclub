package web

import (
	"errors"
	"net/http"
	"strconv"

	"musiclub/internal/store"
)

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	s.renderAdmin(w, r, http.StatusOK, "")
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, status int, msg string) {
	players, err := s.players.List(r.Context(), clubFrom(r.Context()).ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, status, "admin", page{Title: "Admin", Error: msg, Data: players})
}

// handleAdminPlayers is the single endpoint for player management; the form's
// "action" field picks the operation.
func (s *Server) handleAdminPlayers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clubID := clubFrom(ctx).ID

	var err error
	switch action := r.FormValue("action"); action {
	case "add":
		_, err = s.players.Create(ctx, clubID, r.FormValue("name"), r.FormValue("is_admin") != "")
	case "activate", "deactivate", "grant_admin", "revoke_admin":
		var id int64
		id, err = strconv.ParseInt(r.FormValue("player_id"), 10, 64)
		if err != nil {
			err = store.ErrNotFound
			break
		}
		switch action {
		case "activate":
			err = s.players.SetActive(ctx, clubID, id, true)
		case "deactivate":
			err = s.players.SetActive(ctx, clubID, id, false)
		case "grant_admin":
			err = s.players.SetAdmin(ctx, clubID, id, true)
		case "revoke_admin":
			err = s.players.SetAdmin(ctx, clubID, id, false)
		}
	default:
		s.renderAdmin(w, r, http.StatusBadRequest, "Unknown action.")
		return
	}

	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/admin"), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound):
		s.renderAdmin(w, r, http.StatusNotFound, "No such player in this club.")
	case errors.Is(err, store.ErrPlayerNameRequired),
		errors.Is(err, store.ErrPlayerNameTooLong),
		errors.Is(err, store.ErrPlayerNameTaken),
		errors.Is(err, store.ErrLastAdmin):
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		s.serverError(w, r, err)
	}
}

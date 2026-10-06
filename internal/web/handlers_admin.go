package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"musiclub/internal/domain"
	"musiclub/internal/rounds"
	"musiclub/internal/store"
)

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	s.renderAdmin(w, r, http.StatusOK, "")
}

type adminSubmission struct {
	ID         int64
	PlayerName string
}

type adminView struct {
	Players      []domain.Player
	Round        *roundView // the round in progress, if any
	Submissions  []adminSubmission
	DefaultMonth string     // pre-filled in the "start a round" form
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, status int, msg string) {
	ctx := r.Context()
	club := clubFrom(ctx)
	players, err := s.players.List(ctx, club.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	view := adminView{Players: players, DefaultMonth: time.Now().In(s.loc).Format("2006-01")}

	round, err := s.rounds.Current(ctx, club.ID)
	switch {
	case err == nil:
		v, err := s.roundView(ctx, round)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		view.Round = &v

		subs, err := s.roundSvc.ListSubmissions(ctx, club.ID, round.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		names := make(map[int64]string)
		for _, p := range players {
			names[p.ID] = p.Name
		}
		for _, sub := range subs {
			view.Submissions = append(view.Submissions, adminSubmission{
				ID:         sub.ID,
				PlayerName: names[sub.PlayerID],
			})
		}
	case !errors.Is(err, store.ErrNotFound):
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, status, "admin", page{Title: "Admin", Error: msg, Data: view})
}

// handleAdminStartRound kicks off the round for a month; a picker is chosen at random.
func (s *Server) handleAdminStartRound(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, err := s.roundSvc.StartRound(ctx, clubFrom(ctx).ID, strings.TrimSpace(r.FormValue("month")))
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/admin"), http.StatusSeeOther)
	case errors.Is(err, rounds.ErrInvalidMonth),
		errors.Is(err, store.ErrMonthTaken),
		errors.Is(err, store.ErrRoundOpen),
		errors.Is(err, store.ErrNoPlayers):
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		s.serverError(w, r, err)
	}
}

// handleAdminReroll picks a different theme picker while the theme is still pending.
func (s *Server) handleAdminReroll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}
	_, err := s.roundSvc.RerollPicker(ctx, round.ClubID, round.ID)
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/admin"), http.StatusSeeOther)
	case errors.Is(err, store.ErrWrongPhase):
		s.renderAdmin(w, r, http.StatusConflict, "The theme has already been set, so the picker can't be re-rolled.")
	case errors.Is(err, store.ErrNoOtherPicker):
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		s.serverError(w, r, err)
	}
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

// handleAdminDeleteSubmission deletes a player's submission.
func (s *Server) handleAdminDeleteSubmission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Invalid submission ID.")
		return
	}

	err = s.roundSvc.DeleteSubmission(ctx, round.ClubID, round.ID, id)
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/admin"), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, http.StatusNotFound, "Submission not found.")
	default:
		s.serverError(w, r, err)
	}
}

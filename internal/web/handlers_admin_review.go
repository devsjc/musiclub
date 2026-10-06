package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"musiclub/internal/domain"
	"musiclub/internal/rounds"
	"musiclub/internal/store"
)

func (s *Server) handleAdminReview(w http.ResponseWriter, r *http.Request) {
	s.renderReview(w, r, http.StatusOK, "")
}

func (s *Server) renderReview(w http.ResponseWriter, r *http.Request, status int, msg string) {
	ctx := r.Context()
	club := clubFrom(ctx)
	month := r.PathValue("month")
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	if round.Phase != domain.PhaseReview && round.Phase != domain.PhaseClosed {
		http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month, http.StatusSeeOther)
		return
	}

	data, err := s.roundSvc.Review(ctx, club.ID, round.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	s.render(w, r, status, "review", page{
		Title: "Review " + month,
		Error: msg,
		Data:  data,
	})
}

func (s *Server) handleAdminSongKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)
	month := r.PathValue("month")
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	subID, err := strconv.ParseInt(r.FormValue("submission_id"), 10, 64)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "Invalid submission ID")
		return
	}
	key := r.FormValue("song_key")

	err = s.roundSvc.SetSongKey(ctx, club.ID, round.ID, subID, key)
	if err != nil {
		s.renderReview(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/admin/rounds/"+month+"/review", http.StatusSeeOther)
}

func (s *Server) handleAdminQuotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)
	month := r.PathValue("month")
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	action := r.FormValue("action")
	if action == "delete" {
		quoteID, err := strconv.ParseInt(r.FormValue("quote_id"), 10, 64)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "Invalid quote ID")
			return
		}
		if err := s.roundSvc.DeleteFeaturedQuote(ctx, club.ID, round.ID, quoteID); err != nil {
			s.serverError(w, r, err)
			return
		}
	} else {
		subID, err := strconv.ParseInt(r.FormValue("submission_id"), 10, 64)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "Invalid submission ID")
			return
		}
		cat := domain.QuoteCategory(strings.TrimSpace(r.FormValue("category")))
		quote := r.FormValue("quote")
		var ballotID *int64
		if bID, err := strconv.ParseInt(r.FormValue("ballot_id"), 10, 64); err == nil && bID > 0 {
			ballotID = &bID
		}

		if err := s.roundSvc.AddFeaturedQuote(ctx, club.ID, round.ID, subID, cat, ballotID, quote); err != nil {
			s.renderReview(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/admin/rounds/"+month+"/review", http.StatusSeeOther)
}

func (s *Server) handleAdminPublish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)
	month := r.PathValue("month")
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	force := r.FormValue("confirm_uncurated") == "1"
	err := s.roundSvc.Publish(ctx, club.ID, round.ID, force)
	switch {
	case err == nil:
		http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month+"/results", http.StatusSeeOther)
	case errors.Is(err, rounds.ErrQuotesNotCurated):
		s.renderReview(w, r, http.StatusConflict, "No quotes have been featured yet. Check 'Publish anyway' to proceed without quotes.")
	case errors.Is(err, store.ErrWrongPhase):
		s.renderReview(w, r, http.StatusConflict, err.Error())
	default:
		s.serverError(w, r, err)
	}
}

func (s *Server) handleAdminRecompute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)
	month := r.PathValue("month")
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	err := s.roundSvc.Recompute(ctx, club.ID, round.ID)
	if err != nil {
		s.renderReview(w, r, http.StatusBadRequest, err.Error())
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month+"/results", http.StatusSeeOther)
}

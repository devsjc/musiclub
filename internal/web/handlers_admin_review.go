package web

import (
	"musiclub/internal/domain"
	"net/http"
	"strconv"
)

func (s *Server) handleAdminReview(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())
	month := r.PathValue("month")
	round, err := s.rounds.ByMonth(r.Context(), club.ID, month)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if round.Phase != domain.PhaseReview {
		http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month, http.StatusSeeOther)
		return
	}

	subs, _ := s.submissions.List(r.Context(), club.ID, round.ID)

	s.render(w, r, http.StatusOK, "review", page{
		Title: "Review",
		Data: map[string]any{
			"Round":       round,
			"Submissions": subs,
		},
	})
}

func (s *Server) handleAdminSongKeys(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())
	month := r.PathValue("month")
	round, err := s.rounds.ByMonth(r.Context(), club.ID, month)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	subID, _ := strconv.ParseInt(r.FormValue("submission_id"), 10, 64)
	newKey := r.FormValue("song_key")

	err = s.roundSvc.SetSongKey(r.Context(), club.ID, round.ID, subID, newKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/admin/rounds/"+month+"/review", http.StatusSeeOther)
}

func (s *Server) handleAdminQuotes(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())
	month := r.PathValue("month")
	round, err := s.rounds.ByMonth(r.Context(), club.ID, month)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	err = s.roundSvc.SetQuotes(r.Context(), club.ID, round.ID, []domain.FeaturedQuote{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/admin/rounds/"+month+"/review", http.StatusSeeOther)
}

func (s *Server) handleAdminPublish(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())
	month := r.PathValue("month")
	round, err := s.rounds.ByMonth(r.Context(), club.ID, month)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	err = s.roundSvc.Publish(r.Context(), club.ID, round.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month+"/results", http.StatusSeeOther)
}

func (s *Server) handleAdminRecompute(w http.ResponseWriter, r *http.Request) {
	club := clubFrom(r.Context())
	month := r.PathValue("month")
	round, err := s.rounds.ByMonth(r.Context(), club.ID, month)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	err = s.roundSvc.Recompute(r.Context(), club.ID, round.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month+"/results", http.StatusSeeOther)
}

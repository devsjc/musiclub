package web

import (
	"net/http"
	"strconv"

	"musiclub/internal/domain"
)

func (s *Server) handleRoundResults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)

	month := r.PathValue("month")
	rnd, err := s.rounds.ByMonth(ctx, club.ID, month)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Not found")
		return
	}
	if rnd.Phase != domain.PhaseClosed {
		http.Redirect(w, r, "/c/"+club.Slug+"/rounds/"+month, http.StatusSeeOther)
		return
	}

	summary, err := s.stats.MonthSummary(ctx, rnd.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	s.render(w, r, http.StatusOK, "results", page{
		Title: "Results - " + rnd.Month,
		Data: map[string]any{
			"Round":   rnd,
			"Summary": summary,
		},
	})
}

func (s *Server) handleClubStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)

	cs, err := s.stats.ClubStats(ctx, club.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	s.render(w, r, http.StatusOK, "stats", page{
		Title: "Stats - " + club.Name,
		Data: map[string]any{
			"ClubStats": cs,
		},
	})
}

func (s *Server) handlePlayerStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	club := clubFrom(ctx)

	idStr := r.PathValue("id")
	playerID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Not found")
		return
	}

	profile, err := s.stats.PlayerProfile(ctx, club.ID, playerID)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Not found")
		return
	}

	s.render(w, r, http.StatusOK, "player_stats", page{
		Title: "Stats - " + profile.Player.Name,
		Data: map[string]any{
			"Profile": profile,
		},
	})
}

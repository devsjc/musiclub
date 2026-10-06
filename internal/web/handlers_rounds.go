package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
	"musiclub/internal/rounds"
	"musiclub/internal/store"
)

// roundView is a round plus what its templates need to know about it.
type roundView struct {
	domain.Round
	Picker         domain.Player
	ViewerIsPicker bool
	SubmissionsDue string // formatted in the configured zone; empty until set
}

func (s *Server) roundView(ctx context.Context, r domain.Round) (roundView, error) {
	picker, err := s.players.Get(ctx, r.ClubID, r.PickerID)
	if err != nil {
		return roundView{}, err
	}
	v := roundView{Round: r, Picker: picker}
	if p := playerFrom(ctx); p != nil {
		v.ViewerIsPicker = p.ID == r.PickerID
	}
	if r.SubmissionsDueAt != nil {
		v.SubmissionsDue = r.SubmissionsDueAt.In(s.loc).Format("Mon 2 Jan, 15:04")
	}
	return v, nil
}

// roundByMonth loads the round named in the path, writing a 404 if there isn't one.
func (s *Server) roundByMonth(w http.ResponseWriter, r *http.Request) (domain.Round, bool) {
	round, err := s.rounds.ByMonth(r.Context(), clubFrom(r.Context()).ID, r.PathValue("month"))
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, http.StatusNotFound, "No such round.")
		return domain.Round{}, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return domain.Round{}, false
	}
	return round, true
}

// songForm is the submission form's fields: the viewer's saved song, or what
// they just posted if it was rejected.
type songForm struct {
	Artist, Title, URL, Note string
	Saved                    bool // the viewer has a song in this round
}

// submitter is one line of the "who has submitted" checklist: a name and a tick, never the song.
type submitter struct {
	Name string
	Done bool
}

// ballotView holds the data for the voting phase form.
type ballotView struct {
	Submissions []domain.Submission
	Players     []domain.Player // submitters who can be guessed
	Voters      []submitter     // checklist of who has voted
	Ballot      *domain.Ballot  // nil if the viewer hasn't voted yet
	VotingOpen  bool            // false if the deadline passed
	VotingDue   string
}

// roundPage is what the round page template renders.
type roundPage struct {
	roundView
	Form       songForm
	SubmitOpen bool // the submitting phase is still within its deadline
	Submitters []submitter
	BallotView *ballotView
	Guesses    url.Values
}

func (s *Server) handleRound(w http.ResponseWriter, r *http.Request) {
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}
	s.renderRound(w, r, http.StatusOK, round, "")
}

func (s *Server) renderRound(w http.ResponseWriter, r *http.Request, status int, round domain.Round, msg string) {
	s.renderRoundForm(w, r, status, round, msg, nil)
}

// renderRoundForm renders the round page; a non-nil form replaces the viewer's
// saved song in the submission form (used to keep what they typed after an error).
func (s *Server) renderRoundForm(w http.ResponseWriter, r *http.Request, status int, round domain.Round, msg string, form *songForm) {
	ctx := r.Context()
	v, err := s.roundView(ctx, round)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	view := roundPage{roundView: v}

	if round.Phase == domain.PhaseSubmitting {
		view.SubmitOpen = round.SubmissionsDueAt != nil && time.Now().Before(*round.SubmissionsDueAt)
		if view.Submitters, err = s.submitterList(ctx, round); err != nil {
			s.serverError(w, r, err)
			return
		}
		if player := playerFrom(ctx); player != nil {
			saved, err := s.submissions.ByPlayer(ctx, round.ClubID, round.ID, player.ID)
			switch {
			case err == nil:
				view.Form = songForm{Artist: saved.Artist, Title: saved.Title, URL: saved.URL, Note: saved.Note, Saved: true}
			case !errors.Is(err, store.ErrNotFound):
				s.serverError(w, r, err)
				return
			}
		}
		if form != nil {
			form.Saved = view.Form.Saved
			view.Form = *form
		}
	} else if round.Phase == domain.PhaseVoting {
		bv := &ballotView{
			VotingOpen: round.VotingDueAt != nil && time.Now().Before(*round.VotingDueAt),
		}
		if round.VotingDueAt != nil {
			bv.VotingDue = round.VotingDueAt.In(s.loc).Format("Mon 2 Jan, 15:04")
		}
		subs, err := s.submissions.List(ctx, round.ClubID, round.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		bv.Submissions = subs

		players, err := s.players.List(ctx, round.ClubID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		done, err := s.submissions.Submitters(ctx, round.ClubID, round.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, p := range players {
			if done[p.ID] {
				bv.Players = append(bv.Players, p)
			}
		}

		if bv.Voters, err = s.voterList(ctx, round); err != nil {
			s.serverError(w, r, err)
			return
		}

		if player := playerFrom(ctx); player != nil {
			b, err := s.ballots.ByVoter(ctx, round.ClubID, round.ID, player.ID)
			if err == nil {
				bv.Ballot = &b
			} else if !errors.Is(err, store.ErrNotFound) {
				s.serverError(w, r, err)
				return
			}
		}
		view.BallotView = bv
	}
	s.render(w, r, status, "round", page{Title: round.Month, Error: msg, Data: view})
}

// voterList is every active player in the club, ticked if they have voted.
func (s *Server) voterList(ctx context.Context, round domain.Round) ([]submitter, error) {
	players, err := s.players.List(ctx, round.ClubID)
	if err != nil {
		return nil, err
	}
	done, err := s.ballots.Voters(ctx, round.ClubID, round.ID)
	if err != nil {
		return nil, err
	}
	var out []submitter
	for _, p := range players {
		if p.Active || done[p.ID] {
			out = append(out, submitter{Name: p.Name, Done: done[p.ID]})
		}
	}
	return out, nil
}

// submitterList is every active player in the club, ticked if they have submitted.
func (s *Server) submitterList(ctx context.Context, round domain.Round) ([]submitter, error) {
	players, err := s.players.List(ctx, round.ClubID)
	if err != nil {
		return nil, err
	}
	done, err := s.submissions.Submitters(ctx, round.ClubID, round.ID)
	if err != nil {
		return nil, err
	}
	var out []submitter
	for _, p := range players {
		if p.Active || done[p.ID] {
			out = append(out, submitter{Name: p.Name, Done: done[p.ID]})
		}
	}
	return out, nil
}

// handleSubmission saves the current player's song for the round.
func (s *Server) handleSubmission(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	player := playerFrom(ctx)
	if player == nil {
		http.Redirect(w, r, clubPath(ctx, "/whoami"), http.StatusSeeOther)
		return
	}
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	form := songForm{Artist: r.FormValue("artist"), Title: r.FormValue("title"), URL: r.FormValue("url"), Note: r.FormValue("note")}
	_, err := s.roundSvc.SubmitSong(ctx, round.ClubID, round.ID, player.ID,
		rounds.Song{Artist: form.Artist, Title: form.Title, URL: form.URL, Note: form.Note})
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/rounds/"+round.Month), http.StatusSeeOther)
	case errors.Is(err, store.ErrWrongPhase), errors.Is(err, store.ErrDeadlinePassed):
		s.renderRound(w, r, http.StatusConflict, round, "Submissions aren't open for this round.")
	case errors.Is(err, store.ErrAlreadySubmitted):
		s.renderRound(w, r, http.StatusConflict, round, "You have already submitted a song.")
	case errors.Is(err, rounds.ErrArtistRequired),
		errors.Is(err, rounds.ErrTitleRequired),
		errors.Is(err, rounds.ErrSongFieldLong),
		errors.Is(err, rounds.ErrSongUnreadable),
		errors.Is(err, rounds.ErrNoteTooLong),
		errors.Is(err, linkresolver.ErrURLRequired),
		errors.Is(err, linkresolver.ErrURLTooLong),
		errors.Is(err, linkresolver.ErrURLInvalid):
		s.renderRoundForm(w, r, http.StatusUnprocessableEntity, round, err.Error(), &form)
	default:
		s.serverError(w, r, err)
	}
}

// handleSetTheme lets the round's picker set the theme, which opens submissions.
func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	player := playerFrom(ctx)
	if player == nil {
		http.Redirect(w, r, clubPath(ctx, "/whoami"), http.StatusSeeOther)
		return
	}
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}

	err := s.roundSvc.SetTheme(ctx, round.ClubID, round.ID, player.ID, r.FormValue("theme"), r.FormValue("description"))
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/rounds/"+round.Month), http.StatusSeeOther)
	case errors.Is(err, store.ErrNotPicker):
		s.fail(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrWrongPhase):
		s.renderRound(w, r, http.StatusConflict, round, "The theme has already been set.")
	case errors.Is(err, rounds.ErrThemeRequired),
		errors.Is(err, rounds.ErrThemeTooLong),
		errors.Is(err, rounds.ErrDescriptionTooLong):
		s.renderRound(w, r, http.StatusUnprocessableEntity, round, err.Error())
	default:
		s.serverError(w, r, err)
	}
}

// handleBallot saves the voter's ballot and guesses.
func (s *Server) handleBallot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	player := playerFrom(ctx)
	if player == nil {
		http.Redirect(w, r, clubPath(ctx, "/whoami"), http.StatusSeeOther)
		return
	}
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.serverError(w, r, err)
		return
	}
	
	// Create domain.Ballot from form
	// We'll let a new rounds.Service method handle the business logic
	err := s.roundSvc.SubmitBallot(ctx, round.ClubID, round.ID, player.ID, r.Form)
	switch {
	case err == nil:
		http.Redirect(w, r, clubPath(ctx, "/rounds/"+round.Month), http.StatusSeeOther)
	case errors.Is(err, store.ErrWrongPhase), errors.Is(err, store.ErrDeadlinePassed):
		s.renderRound(w, r, http.StatusConflict, round, "Voting isn't open for this round.")
	case errors.Is(err, store.ErrAlreadyVoted):
		s.renderRound(w, r, http.StatusConflict, round, err.Error())
	default:
		// For other validation errors, show them on the form
		if errors.Is(err, rounds.ErrInvalidBallot) {
			s.renderRound(w, r, http.StatusUnprocessableEntity, round, err.Error())
		} else {
			s.serverError(w, r, err)
		}
	}
}

// handleBallotGuesses returns the guesses fragment for HTMX.
func (s *Server) handleBallotGuesses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	round, ok := s.roundByMonth(w, r)
	if !ok {
		return
	}
	
	subs, err := s.submissions.List(ctx, round.ClubID, round.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	
	players, err := s.players.List(ctx, round.ClubID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	done, err := s.submissions.Submitters(ctx, round.ClubID, round.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var guessPlayers []domain.Player
	for _, p := range players {
		if done[p.ID] {
			guessPlayers = append(guessPlayers, p)
		}
	}
	
	_ = r.ParseForm()
	view := roundPage{
		roundView:  roundView{Round: round},
		BallotView: &ballotView{Submissions: subs, Players: guessPlayers},
		Guesses:    r.Form,
	}

	s.renderFragment(w, r, http.StatusOK, "guesses_fragment", page{
		Title: round.Month,
		Data:  view,
	})
}

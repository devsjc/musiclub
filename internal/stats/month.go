package stats

import (
	"context"
	"database/sql"
	"musiclub/internal/domain"
	"musiclub/internal/scoring"
	"time"
)

const sqliteTime = "2006-01-02 15:04:05"

func parseTime(s string) time.Time {
	t, _ := time.ParseInLocation(sqliteTime, s, time.UTC)
	return t
}

func (s *Store) roundPoints(ctx context.Context, roundID int64) ([]PlayerPoints, error) {
	q := `SELECT p.id, p.name, e.kind, e.points
		  FROM players p
		  JOIN score_events e ON e.player_id = p.id
		  WHERE e.round_id = ?
		  ORDER BY p.name`

	rows, err := s.db.QueryContext(ctx, q, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byPlayer := make(map[int64]*PlayerPoints)
	var order []int64

	for rows.Next() {
		var pID int64
		var name string
		var kind string
		var points int
		if err := rows.Scan(&pID, &name, &kind, &points); err != nil {
			return nil, err
		}

		pp, ok := byPlayer[pID]
		if !ok {
			pp = &PlayerPoints{
				PlayerID: pID,
				Name:     name,
			}
			byPlayer[pID] = pp
			order = append(order, pID)
		}

		switch domain.ScoreKind(kind) {
		case domain.ScoreFavoriteVote:
			pp.FavoriteVotes += points
		case domain.ScoreBestFitVote:
			pp.BestFitVotes += points
		case domain.ScoreFavoriteWinner:
			pp.FavoriteWins += points
		case domain.ScoreBestFitWinner:
			pp.BestFitWins += points
		case domain.ScoreTrendsetter:
			pp.Trendsetter += points
		}
		pp.Total += points
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	res := make([]PlayerPoints, 0, len(order))
	for _, id := range order {
		res = append(res, *byPlayer[id])
	}
	// Sort by total desc, then name
	for i := 0; i < len(res)-1; i++ {
		for j := i + 1; j < len(res); j++ {
			if res[i].Total < res[j].Total || (res[i].Total == res[j].Total && res[i].Name > res[j].Name) {
				res[i], res[j] = res[j], res[i]
			}
		}
	}

	return res, nil
}

func (s *Store) roundGuessStats(ctx context.Context, roundID int64) (GuessStats, error) {
	var gs GuessStats

	// Guessability (by submitter)
	q1 := `SELECT s.player_id, p.name,
				  COUNT(*) AS total,
				  SUM(g.guessed_player_id = s.player_id) AS correct
		   FROM guesses g
		   JOIN ballots b ON b.id = g.ballot_id
		   JOIN submissions s ON s.id = g.submission_id
		   JOIN players p ON p.id = s.player_id
		   WHERE b.round_id = ?
		   GROUP BY s.player_id, p.name
		   ORDER BY (SUM(g.guessed_player_id = s.player_id)*100.0/COUNT(*)) DESC, p.name`

	rows1, err := s.db.QueryContext(ctx, q1, roundID)
	if err != nil {
		return gs, err
	}
	defer rows1.Close()

	var overallCorrect, overallTotal int
	for rows1.Next() {
		var st SubmitterStat
		if err := rows1.Scan(&st.PlayerID, &st.Name, &st.Total, &st.Correct); err != nil {
			return gs, err
		}
		if st.Total > 0 {
			st.Pct = float64(st.Correct) * 100 / float64(st.Total)
		}
		overallCorrect += st.Correct
		overallTotal += st.Total
		gs.BySubmitter = append(gs.BySubmitter, st)
	}

	if overallTotal > 0 {
		gs.OverallCorrectPct = float64(overallCorrect) * 100 / float64(overallTotal)
	}

	// Guesser stats
	q2 := `SELECT b.voter_id, p.name,
				  COUNT(*) AS total,
				  SUM(g.guessed_player_id = s.player_id) AS correct
		   FROM guesses g
		   JOIN ballots b ON b.id = g.ballot_id
		   JOIN submissions s ON s.id = g.submission_id
		   JOIN players p ON p.id = b.voter_id
		   WHERE b.round_id = ?
		   GROUP BY b.voter_id, p.name
		   ORDER BY (SUM(g.guessed_player_id = s.player_id)*100.0/COUNT(*)) DESC, p.name`

	rows2, err := s.db.QueryContext(ctx, q2, roundID)
	if err != nil {
		return gs, err
	}
	defer rows2.Close()

	for rows2.Next() {
		var gst GuesserStat
		if err := rows2.Scan(&gst.PlayerID, &gst.Name, &gst.Total, &gst.Correct); err != nil {
			return gs, err
		}
		if gst.Total > 0 {
			gst.Pct = float64(gst.Correct) * 100 / float64(gst.Total)
		}
		gs.ByGuesser = append(gs.ByGuesser, gst)
	}

	return gs, nil
}

func (s *Store) roundQuotes(ctx context.Context, roundID int64) ([]QuoteView, error) {
	q := `SELECT q.id, q.round_id, q.category, q.submission_id, q.ballot_id, q.quote, q.position,
				 p_sub.name AS submitter_name,
				 p_voter.name AS voter_name
		  FROM featured_quotes q
		  JOIN submissions s ON s.id = q.submission_id
		  JOIN players p_sub ON p_sub.id = s.player_id
		  LEFT JOIN ballots b ON b.id = q.ballot_id
		  LEFT JOIN players p_voter ON p_voter.id = b.voter_id
		  WHERE q.round_id = ?
		  ORDER BY q.category, q.position, q.id`

	rows, err := s.db.QueryContext(ctx, q, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []QuoteView
	for rows.Next() {
		var qv QuoteView
		var ballotID sql.NullInt64
		var voterName sql.NullString

		err := rows.Scan(&qv.Quote.ID, &qv.Quote.RoundID, &qv.Quote.Category, &qv.Quote.SubmissionID,
			&ballotID, &qv.Quote.Quote, &qv.Quote.Position,
			&qv.SubmitterName, &voterName)
		if err != nil {
			return nil, err
		}

		if ballotID.Valid {
			id := ballotID.Int64
			qv.Quote.BallotID = &id
			qv.VoterName = voterName.String
		}
		res = append(res, qv)
	}
	return res, nil
}

func (s *Store) roundWinners(ctx context.Context, roundID int64) (Winners, error) {
	w, _, err := s.roundWinnersAndSongs(ctx, roundID)
	return w, err
}

func (s *Store) roundWinnersAndSongs(ctx context.Context, roundID int64) (Winners, []SongReveal, error) {
	subQ := `SELECT s.id, s.round_id, s.player_id, s.title, s.artist, s.url, s.url_service,
				    s.link_source, s.external_id, s.note, s.song_key, s.submitted_at, s.position,
					p.name
			 FROM submissions s
			 JOIN players p ON p.id = s.player_id
			 WHERE s.round_id = ?
			 ORDER BY COALESCE(s.position, s.id), s.id`

	subRows, err := s.db.QueryContext(ctx, subQ, roundID)
	if err != nil {
		return Winners{}, nil, err
	}
	defer subRows.Close()

	var songReveals []SongReveal
	scoreSubs := []scoring.Submission{}
	names := make(map[int64]string)
	subByID := make(map[int64]domain.Submission)

	for subRows.Next() {
		var sub domain.Submission
		var name string
		var extID, note sql.NullString
		var pos sql.NullInt64
		var submittedAt string

		err := subRows.Scan(&sub.ID, &sub.RoundID, &sub.PlayerID, &sub.Title, &sub.Artist,
			&sub.URL, &sub.URLService, &sub.LinkSource, &extID, &note, &sub.SongKey,
			&submittedAt, &pos, &name)
		if err != nil {
			return Winners{}, nil, err
		}

		sub.ExternalID = extID.String
		sub.Note = note.String
		sub.SubmittedAt = parseTime(submittedAt)
		if pos.Valid {
			v := int(pos.Int64)
			sub.Position = &v
		}

		subByID[sub.ID] = sub
		names[sub.ID] = name
		songReveals = append(songReveals, SongReveal{
			Submission:    sub,
			SubmitterName: name,
		})
		scoreSubs = append(scoreSubs, scoring.Submission{
			ID:          sub.ID,
			PlayerID:    sub.PlayerID,
			SongKey:     sub.SongKey,
			SubmittedAt: sub.SubmittedAt,
		})
	}
	if err := subRows.Err(); err != nil {
		return Winners{}, nil, err
	}

	balQ := `SELECT id, voter_id, favorite_submission_id, best_fit_submission_id
			 FROM ballots WHERE round_id = ?`
	balRows, err := s.db.QueryContext(ctx, balQ, roundID)
	if err != nil {
		return Winners{}, nil, err
	}
	defer balRows.Close()

	var scoreBals []scoring.Ballot
	for balRows.Next() {
		var b scoring.Ballot
		if err := balRows.Scan(&b.ID, &b.VoterID, &b.FavoriteSubmissionID, &b.BestFitSubmissionID); err != nil {
			return Winners{}, nil, err
		}
		scoreBals = append(scoreBals, b)
	}
	if err := balRows.Err(); err != nil {
		return Winners{}, nil, err
	}

	tallies := scoring.Tally(scoring.Results{
		Submissions: scoreSubs,
		Ballots:     scoreBals,
	})

	var w Winners
	for _, id := range tallies.FavoriteWins {
		w.Favorite = append(w.Favorite, WinnerView{
			Submission:    subByID[id],
			SubmitterName: names[id],
			Votes:         tallies.FavoriteVotes[id],
		})
	}
	for _, id := range tallies.BestFitWins {
		w.BestFit = append(w.BestFit, WinnerView{
			Submission:    subByID[id],
			SubmitterName: names[id],
			Votes:         tallies.BestFitVotes[id],
		})
	}
	for _, id := range tallies.OverallWins {
		w.Overall = append(w.Overall, WinnerView{
			Submission:    subByID[id],
			SubmitterName: names[id],
			Votes:         tallies.OverallVotes[id],
		})
	}

	return w, songReveals, nil
}

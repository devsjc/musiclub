package stats

import (
	"context"
	"sort"
)

func (s *Store) ClubStats(ctx context.Context, clubID int64) (ClubStats, error) {
	var cs ClubStats
	cs.ClubID = clubID

	// Active and historical players
	qP := `SELECT id, name FROM players WHERE club_id = ? ORDER BY name`
	rowsP, err := s.db.QueryContext(ctx, qP, clubID)
	if err != nil {
		return cs, err
	}
	defer rowsP.Close()

	playerNames := make(map[int64]string)
	for rowsP.Next() {
		var id int64
		var name string
		if err := rowsP.Scan(&id, &name); err != nil {
			return cs, err
		}
		playerNames[id] = name
		cs.MatrixPlayers = append(cs.MatrixPlayers, PlayerMatrixHeader{ID: id, Name: name})
	}
	if err := rowsP.Err(); err != nil {
		return cs, err
	}

	// Leaderboard (correlated subqueries prevent Cartesian explosion)
	qL := `SELECT p.id, p.name,
				  COALESCE((SELECT SUM(points) FROM score_events WHERE player_id = p.id), 0) AS total_points,
				  COALESCE((SELECT COUNT(DISTINCT round_id) FROM submissions WHERE player_id = p.id), 0) AS rounds_played
		   FROM players p
		   WHERE p.club_id = ?
		   ORDER BY total_points DESC, rounds_played DESC, p.name`

	rowsL, err := s.db.QueryContext(ctx, qL, clubID)
	if err != nil {
		return cs, err
	}
	defer rowsL.Close()

	for rowsL.Next() {
		var r LeaderboardRow
		if err := rowsL.Scan(&r.PlayerID, &r.Name, &r.TotalPoints, &r.RoundsPlayed); err != nil {
			return cs, err
		}
		if r.RoundsPlayed > 0 {
			r.AvgPerRound = float64(r.TotalPoints) / float64(r.RoundsPlayed)
		}
		cs.Leaderboard = append(cs.Leaderboard, r)
	}
	if err := rowsL.Err(); err != nil {
		return cs, err
	}

	// Honours
	qH := `SELECT p.id, p.name,
				  COALESCE(SUM(CASE WHEN e.kind = 'favorite_winner' THEN 1 ELSE 0 END), 0) AS fav_wins,
				  COALESCE(SUM(CASE WHEN e.kind = 'best_fit_winner' THEN 1 ELSE 0 END), 0) AS bf_wins,
				  COALESCE(SUM(CASE WHEN e.kind = 'trendsetter' THEN e.points ELSE 0 END), 0) AS trend_pts
		   FROM players p
		   LEFT JOIN score_events e ON e.player_id = p.id
		   WHERE p.club_id = ?
		   GROUP BY p.id, p.name
		   ORDER BY (fav_wins + bf_wins) DESC, p.name`

	rowsH, err := s.db.QueryContext(ctx, qH, clubID)
	if err != nil {
		return cs, err
	}
	defer rowsH.Close()

	for rowsH.Next() {
		var h HonourRow
		if err := rowsH.Scan(&h.PlayerID, &h.Name, &h.FavoriteWins, &h.BestFitWins, &h.Trendsetter); err != nil {
			return cs, err
		}
		cs.Honours = append(cs.Honours, h)
	}
	if err := rowsH.Err(); err != nil {
		return cs, err
	}

	// Overall wins computation: read round IDs first, close rows, then calculate winners
	qR := `SELECT id FROM rounds WHERE club_id = ? AND phase = 'closed'`
	rowsR, err := s.db.QueryContext(ctx, qR, clubID)
	if err != nil {
		return cs, err
	}
	var closedRoundIDs []int64
	for rowsR.Next() {
		var rID int64
		if err := rowsR.Scan(&rID); err != nil {
			rowsR.Close()
			return cs, err
		}
		closedRoundIDs = append(closedRoundIDs, rID)
	}
	rowsR.Close()

	overallCounts := make(map[int64]int)
	for _, rID := range closedRoundIDs {
		w, err := s.roundWinners(ctx, rID)
		if err != nil {
			return cs, err
		}
		for _, o := range w.Overall {
			overallCounts[o.Submission.PlayerID]++
		}
	}
	for i := range cs.Honours {
		cs.Honours[i].OverallWins = overallCounts[cs.Honours[i].PlayerID]
	}

	// Cumulative Guess Stats:
	// Guesser = b.voter_id
	// Submitter whose song is guessed = s.player_id
	qG := `SELECT s.player_id AS sub_player_id, b.voter_id AS guesser_id,
				  SUM(g.guessed_player_id = s.player_id) AS correct,
				  COUNT(*) AS total
		   FROM guesses g
		   JOIN ballots b ON b.id = g.ballot_id
		   JOIN submissions s ON s.id = g.submission_id
		   JOIN rounds r ON r.id = b.round_id
		   WHERE r.club_id = ? AND r.phase = 'closed'
		   GROUP BY s.player_id, b.voter_id`

	rowsG, err := s.db.QueryContext(ctx, qG, clubID)
	if err != nil {
		return cs, err
	}
	defer rowsG.Close()

	cs.GuessMatrix = make(map[int64]map[int64]float64)

	submitters := make(map[int64]*SubmitterStat)
	guessers := make(map[int64]*GuesserStat)
	var overCorrect, overTotal int

	for rowsG.Next() {
		var subID, gID int64
		var correct, total int
		if err := rowsG.Scan(&subID, &gID, &correct, &total); err != nil {
			return cs, err
		}

		if _, ok := cs.GuessMatrix[gID]; !ok {
			cs.GuessMatrix[gID] = make(map[int64]float64)
		}
		if total > 0 {
			cs.GuessMatrix[gID][subID] = float64(correct) * 100 / float64(total)
		}

		if _, ok := submitters[subID]; !ok {
			submitters[subID] = &SubmitterStat{PlayerID: subID, Name: playerNames[subID]}
		}
		submitters[subID].Correct += correct
		submitters[subID].Total += total

		if _, ok := guessers[gID]; !ok {
			guessers[gID] = &GuesserStat{PlayerID: gID, Name: playerNames[gID]}
		}
		guessers[gID].Correct += correct
		guessers[gID].Total += total

		overCorrect += correct
		overTotal += total
	}
	if err := rowsG.Err(); err != nil {
		return cs, err
	}

	if overTotal > 0 {
		cs.GuessStats.OverallCorrectPct = float64(overCorrect) * 100 / float64(overTotal)
	}
	for _, s := range submitters {
		if s.Total > 0 {
			s.Pct = float64(s.Correct) * 100 / float64(s.Total)
		}
		cs.GuessStats.BySubmitter = append(cs.GuessStats.BySubmitter, *s)
	}
	sort.Slice(cs.GuessStats.BySubmitter, func(i, j int) bool {
		if cs.GuessStats.BySubmitter[i].Pct == cs.GuessStats.BySubmitter[j].Pct {
			return cs.GuessStats.BySubmitter[i].Name < cs.GuessStats.BySubmitter[j].Name
		}
		return cs.GuessStats.BySubmitter[i].Pct > cs.GuessStats.BySubmitter[j].Pct
	})

	for _, g := range guessers {
		if g.Total > 0 {
			g.Pct = float64(g.Correct) * 100 / float64(g.Total)
		}
		cs.GuessStats.ByGuesser = append(cs.GuessStats.ByGuesser, *g)
	}
	sort.Slice(cs.GuessStats.ByGuesser, func(i, j int) bool {
		if cs.GuessStats.ByGuesser[i].Pct == cs.GuessStats.ByGuesser[j].Pct {
			return cs.GuessStats.ByGuesser[i].Name < cs.GuessStats.ByGuesser[j].Name
		}
		return cs.GuessStats.ByGuesser[i].Pct > cs.GuessStats.ByGuesser[j].Pct
	})

	return cs, nil
}

func (s *Store) PlayerProfile(ctx context.Context, clubID, playerID int64) (PlayerProfile, error) {
	var profile PlayerProfile

	// Player info
	err := s.db.QueryRowContext(ctx,
		`SELECT id, club_id, name, active, is_admin FROM players WHERE club_id = ? AND id = ?`,
		clubID, playerID).Scan(&profile.Player.ID, &profile.Player.ClubID, &profile.Player.Name, &profile.Player.Active, &profile.Player.IsAdmin)
	if err != nil {
		return profile, err
	}

	// Points & Rounds
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE((SELECT SUM(points) FROM score_events WHERE player_id = ?), 0),
		        COALESCE((SELECT COUNT(DISTINCT round_id) FROM submissions WHERE player_id = ?), 0)`,
		playerID, playerID).Scan(&profile.TotalPoints, &profile.RoundsPlayed)

	// Honours
	_ = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN kind = 'favorite_winner' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN kind = 'best_fit_winner' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN kind = 'trendsetter' THEN points ELSE 0 END), 0)
		 FROM score_events WHERE player_id = ?`,
		playerID).Scan(&profile.Honours.FavoriteWins, &profile.Honours.BestFitWins, &profile.Honours.Trendsetter)
	profile.Honours.PlayerID = playerID
	profile.Honours.Name = profile.Player.Name

	// Submissions from closed rounds
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.month, s.title, s.artist, s.url
		 FROM submissions s
		 JOIN rounds r ON r.id = s.round_id
		 WHERE s.player_id = ? AND r.phase = 'closed'
		 ORDER BY r.month DESC`, playerID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sub PlayerSongReveal
			if err := rows.Scan(&sub.Month, &sub.Title, &sub.Artist, &sub.URL); err == nil {
				profile.Songs = append(profile.Songs, sub)
			}
		}
	}

	return profile, nil
}

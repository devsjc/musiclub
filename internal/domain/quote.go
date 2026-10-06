package domain

type QuoteCategory string

const (
	QuoteCategoryFavorite QuoteCategory = "favorite"
	QuoteCategoryBestFit  QuoteCategory = "best_fit"
)

type FeaturedQuote struct {
	ID           int64
	RoundID      int64
	Category     QuoteCategory
	SubmissionID int64
	BallotID     *int64
	Quote        string
	Position     int
}

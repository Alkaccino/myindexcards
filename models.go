package main

import "time"

// User represents a registered user
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

// Deck represents a collection of cards grouped by topic
type Deck struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Card represents a single flashcard with front (question) and back (answer)
type Card struct {
	ID     string `json:"id"`
	DeckID string `json:"deck_id"`
	UserID string `json:"user_id"`
	Front  string `json:"front"`
	Back   string `json:"back"`
	// Spaced repetition fields
	Ease       float64   `json:"ease"`       // ease factor (default 2.5)
	Interval   int       `json:"interval"`   // days until next review
	Repetition int       `json:"repetition"` // number of successful reviews
	NextReview time.Time `json:"next_review"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ReviewRating represents how well the user knew the answer (1-5)
type ReviewRating int

const (
	RatingBlackout ReviewRating = 1 // Keine Ahnung
	RatingHard     ReviewRating = 2 // Schwer
	RatingOkay     ReviewRating = 3 // Okay
	RatingGood     ReviewRating = 4 // Gut
	RatingPerfect  ReviewRating = 5 // Perfekt
)

// ExportData represents the format for import/export
type ExportData struct {
	Version string `json:"version"`
	Decks   []Deck `json:"decks"`
	Cards   []Card `json:"cards"`
}

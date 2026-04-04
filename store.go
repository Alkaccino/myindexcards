package main

import (
	"context"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Store handles all data persistence using PostgreSQL
type Store struct {
	db       *pgxpool.Pool
	mu       sync.RWMutex
	sessions map[string]string // session token -> user ID (in-memory)
}

// NewStore creates a new store connected to the given PostgreSQL DSN
func NewStore(dsn string) (*Store, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Store{
		db:       pool,
		sessions: make(map[string]string),
	}, nil
}

// --- User Operations ---

func (s *Store) CreateUser(username, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:           uuid.New().String(),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	_, err = s.db.Exec(context.Background(),
		`INSERT INTO users (id, username, password_hash, created_at)
 VALUES ($1, $2, $3, $4)`,
		user.ID, user.Username, user.PasswordHash, user.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return user, nil
}

func (s *Store) AuthenticateUser(username, password string) (*User, error) {
	var user User
	err := s.db.QueryRow(context.Background(),
		`SELECT id, username, password_hash, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &user, nil
}

// --- Session Operations (in-memory) ---

func (s *Store) CreateSession(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := uuid.New().String()
	s.sessions[token] = userID
	return token
}

func (s *Store) GetUserBySession(token string) *User {
	s.mu.RLock()
	userID, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return nil
	}

	var user User
	err := s.db.QueryRow(context.Background(),
		`SELECT id, username, password_hash, created_at FROM users WHERE id = $1`,
		userID,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil
	}
	return &user
}

func (s *Store) DeleteSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// --- Deck Operations ---

func (s *Store) CreateDeck(userID, name, color string) (*Deck, error) {
	deck := &Deck{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      name,
		Color:     color,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_, err := s.db.Exec(context.Background(),
		`INSERT INTO decks (id, user_id, name, color, created_at, updated_at)
 VALUES ($1, $2, $3, $4, $5, $6)`,
		deck.ID, deck.UserID, deck.Name, deck.Color, deck.CreatedAt, deck.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return deck, nil
}

func (s *Store) GetDecksByUser(userID string) []*Deck {
	rows, err := s.db.Query(context.Background(),
		`SELECT id, user_id, name, color, created_at, updated_at
 FROM decks WHERE user_id = $1 ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []*Deck
	for rows.Next() {
		d := &Deck{}
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Color, &d.CreatedAt, &d.UpdatedAt); err == nil {
			result = append(result, d)
		}
	}
	return result
}

func (s *Store) GetDeck(id, userID string) *Deck {
	d := &Deck{}
	err := s.db.QueryRow(context.Background(),
		`SELECT id, user_id, name, color, created_at, updated_at
 FROM decks WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.Color, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil
	}
	return d
}

func (s *Store) UpdateDeck(id, userID, name, color string) error {
	cmd, err := s.db.Exec(context.Background(),
		`UPDATE decks SET name = $1, color = $2, updated_at = $3
 WHERE id = $4 AND user_id = $5`,
		name, color, time.Now(), id, userID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDeck(id, userID string) error {
	cmd, err := s.db.Exec(context.Background(),
		`DELETE FROM decks WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Card Operations ---

func (s *Store) CreateCard(userID, deckID, front, back string) (*Card, error) {
	if s.GetDeck(deckID, userID) == nil {
		return nil, ErrNotFound
	}

	card := &Card{
		ID:         uuid.New().String(),
		DeckID:     deckID,
		UserID:     userID,
		Front:      front,
		Back:       back,
		Ease:       2.5,
		Interval:   0,
		Repetition: 0,
		NextReview: time.Now(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	_, err := s.db.Exec(context.Background(),
		`INSERT INTO cards (id, deck_id, user_id, front, back, ease, interval_days, repetition, next_review, created_at, updated_at)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		card.ID, card.DeckID, card.UserID, card.Front, card.Back,
		card.Ease, card.Interval, card.Repetition, card.NextReview, card.CreatedAt, card.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return card, nil
}

func (s *Store) GetCardsByDeck(deckID, userID string) []*Card {
	rows, err := s.db.Query(context.Background(),
		`SELECT id, deck_id, user_id, front, back, ease, interval_days, repetition, next_review, created_at, updated_at
 FROM cards WHERE deck_id = $1 AND user_id = $2 ORDER BY created_at ASC`,
		deckID, userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []*Card
	for rows.Next() {
		c := &Card{}
		if err := rows.Scan(&c.ID, &c.DeckID, &c.UserID, &c.Front, &c.Back,
			&c.Ease, &c.Interval, &c.Repetition, &c.NextReview, &c.CreatedAt, &c.UpdatedAt); err == nil {
			result = append(result, c)
		}
	}
	return result
}

func (s *Store) GetCard(id, userID string) *Card {
	c := &Card{}
	err := s.db.QueryRow(context.Background(),
		`SELECT id, deck_id, user_id, front, back, ease, interval_days, repetition, next_review, created_at, updated_at
 FROM cards WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&c.ID, &c.DeckID, &c.UserID, &c.Front, &c.Back,
		&c.Ease, &c.Interval, &c.Repetition, &c.NextReview, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil
	}
	return c
}

func (s *Store) UpdateCard(id, userID, front, back string) error {
	cmd, err := s.db.Exec(context.Background(),
		`UPDATE cards SET front = $1, back = $2, updated_at = $3
 WHERE id = $4 AND user_id = $5`,
		front, back, time.Now(), id, userID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCard(id, userID string) error {
	cmd, err := s.db.Exec(context.Background(),
		`DELETE FROM cards WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetDeck resets spaced-repetition progress for all cards in a deck.
func (s *Store) ResetDeck(deckID, userID string) error {
	_, err := s.db.Exec(context.Background(),
		`UPDATE cards
		 SET ease = 2.5, interval_days = 0, repetition = 0, next_review = $3
		 WHERE deck_id = $1 AND user_id = $2`,
		deckID, userID, time.Now(),
	)
	return err
}

// ReviewCard applies spaced repetition and persists the result.
// Ratings 1-2 (Nochmal/Schwer) only update ease; the handler manages session re-queuing.
// Ratings 3-5 (Okay/Gut/Einfach) set next_review in the future.
func (s *Store) ReviewCard(id, userID string, rating ReviewRating) error {
	c := s.GetCard(id, userID)
	if c == nil {
		return ErrNotFound
	}

	now := time.Now()

	switch rating {
	case RatingAgain: // Nochmal
		c.Ease -= 0.20
		if c.Ease < 1.3 {
			c.Ease = 1.3
		}
		c.Interval = 0
		c.Repetition = 0
		c.NextReview = now

	case RatingHard: // Schwer
		c.Ease -= 0.15
		if c.Ease < 1.3 {
			c.Ease = 1.3
		}
		c.Interval = 0
		c.Repetition = 0
		c.NextReview = now

	case RatingOkay: // Okay – 1 Stunde
		c.NextReview = now.Add(1 * time.Hour)
		// interval stays 0 (sub-day), ease & repetition unchanged

	case RatingGood: // Gut – 1-2 Tage, dann wachsend
		switch c.Repetition {
		case 0:
			c.Interval = 1
		case 1:
			c.Interval = 2
		default:
			c.Interval = int(math.Round(float64(c.Interval) * c.Ease))
		}
		c.Repetition++
		c.NextReview = now.AddDate(0, 0, c.Interval)

	case RatingEasy: // Einfach – 5 Tage, dann schneller wachsend
		c.Ease += 0.15
		switch c.Repetition {
		case 0:
			c.Interval = 5
		default:
			c.Interval = int(math.Round(float64(c.Interval) * c.Ease * 1.3))
			if c.Interval < 5 {
				c.Interval = 5
			}
		}
		c.Repetition++
		c.NextReview = now.AddDate(0, 0, c.Interval)
	}

	c.UpdatedAt = now
	_, err := s.db.Exec(context.Background(),
		`UPDATE cards SET ease = $1, interval_days = $2, repetition = $3, next_review = $4, updated_at = $5
 WHERE id = $6 AND user_id = $7`,
		c.Ease, c.Interval, c.Repetition, c.NextReview, c.UpdatedAt, id, userID,
	)
	return err
}

// GetDueCards returns cards that are due for review, shuffled
func (s *Store) GetDueCards(deckID, userID string) []*Card {
	rows, err := s.db.Query(context.Background(),
		`SELECT id, deck_id, user_id, front, back, ease, interval_days, repetition, next_review, created_at, updated_at
 FROM cards WHERE deck_id = $1 AND user_id = $2 AND next_review <= $3`,
		deckID, userID, time.Now(),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []*Card
	for rows.Next() {
		c := &Card{}
		if err := rows.Scan(&c.ID, &c.DeckID, &c.UserID, &c.Front, &c.Back,
			&c.Ease, &c.Interval, &c.Repetition, &c.NextReview, &c.CreatedAt, &c.UpdatedAt); err == nil {
			result = append(result, c)
		}
	}
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result
}

func (s *Store) GetDueCardCount(deckID, userID string) int {
	var count int
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cards WHERE deck_id = $1 AND user_id = $2 AND next_review <= $3`,
		deckID, userID, time.Now(),
	).Scan(&count)
	return count
}

func (s *Store) GetCardCount(deckID, userID string) int {
	var count int
	s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM cards WHERE deck_id = $1 AND user_id = $2`,
		deckID, userID,
	).Scan(&count)
	return count
}

// GetDueCardsMulti returns shuffled due cards from multiple decks
func (s *Store) GetDueCardsMulti(deckIDs []string, userID string) []*Card {
	var result []*Card
	for _, did := range deckIDs {
		result = append(result, s.GetDueCards(did, userID)...)
	}
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result
}

// GetCardsByDecks returns all cards from multiple decks, shuffled
func (s *Store) GetCardsByDecks(deckIDs []string, userID string) []*Card {
	var result []*Card
	for _, did := range deckIDs {
		result = append(result, s.GetCardsByDeck(did, userID)...)
	}
	rand.Shuffle(len(result), func(i, j int) { result[i], result[j] = result[j], result[i] })
	return result
}

// --- Import/Export ---

func (s *Store) ExportDecks(userID string, deckIDs []string) (*ExportData, error) {
	export := &ExportData{Version: "1.0"}
	for _, did := range deckIDs {
		d := s.GetDeck(did, userID)
		if d == nil {
			continue
		}
		export.Decks = append(export.Decks, *d)
		for _, c := range s.GetCardsByDeck(did, userID) {
			export.Cards = append(export.Cards, *c)
		}
	}
	return export, nil
}

func (s *Store) ImportDecks(userID string, data *ExportData) error {
	ctx := context.Background()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	deckMap := make(map[string]string)
	now := time.Now()

	for _, d := range data.Decks {
		newID := uuid.New().String()
		deckMap[d.ID] = newID
		_, err := tx.Exec(ctx,
			`INSERT INTO decks (id, user_id, name, color, created_at, updated_at)
 VALUES ($1, $2, $3, $4, $5, $6)`,
			newID, userID, d.Name, d.Color, now, now,
		)
		if err != nil {
			return err
		}
	}

	for _, c := range data.Cards {
		newDeckID, ok := deckMap[c.DeckID]
		if !ok {
			continue
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO cards (id, deck_id, user_id, front, back, ease, interval_days, repetition, next_review, created_at, updated_at)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			uuid.New().String(), newDeckID, userID, c.Front, c.Back,
			2.5, 0, 0, now, now, now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

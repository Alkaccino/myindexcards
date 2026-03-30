package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Store handles all data persistence using JSON files
type Store struct {
	mu       sync.RWMutex
	dataDir  string
	users    map[string]*User  // keyed by ID
	decks    map[string]*Deck  // keyed by ID
	cards    map[string]*Card  // keyed by ID
	sessions map[string]string // session token -> user ID
}

// NewStore creates a new store and loads existing data
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		dataDir:  dataDir,
		users:    make(map[string]*User),
		decks:    make(map[string]*Deck),
		cards:    make(map[string]*Card),
		sessions: make(map[string]string),
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// --- Persistence ---

type storeData struct {
	Users []*User `json:"users"`
	Decks []*Deck `json:"decks"`
	Cards []*Card `json:"cards"`
}

func (s *Store) filePath() string {
	return filepath.Join(s.dataDir, "store.json")
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var sd storeData
	if err := json.Unmarshal(data, &sd); err != nil {
		return err
	}
	for _, u := range sd.Users {
		s.users[u.ID] = u
	}
	for _, d := range sd.Decks {
		s.decks[d.ID] = d
	}
	for _, c := range sd.Cards {
		s.cards[c.ID] = c
	}
	return nil
}

func (s *Store) save() error {
	sd := storeData{}
	for _, u := range s.users {
		sd.Users = append(sd.Users, u)
	}
	for _, d := range s.decks {
		sd.Decks = append(sd.Decks, d)
	}
	for _, c := range s.cards {
		sd.Cards = append(sd.Cards, c)
	}
	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath(), data, 0600)
}

// --- User Operations ---

func (s *Store) CreateUser(username, password string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if username exists
	for _, u := range s.users {
		if u.Username == username {
			return nil, ErrUserExists
		}
	}

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
	s.users[user.ID] = user
	return user, s.save()
}

func (s *Store) AuthenticateUser(username, password string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.Username == username {
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
				return nil, ErrInvalidCredentials
			}
			return u, nil
		}
	}
	return nil, ErrInvalidCredentials
}

func (s *Store) CreateSession(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := uuid.New().String()
	s.sessions[token] = userID
	return token
}

func (s *Store) GetUserBySession(token string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID, ok := s.sessions[token]
	if !ok {
		return nil
	}
	return s.users[userID]
}

func (s *Store) DeleteSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// --- Deck Operations ---

func (s *Store) CreateDeck(userID, name, color string) (*Deck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deck := &Deck{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      name,
		Color:     color,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.decks[deck.ID] = deck
	return deck, s.save()
}

func (s *Store) GetDecksByUser(userID string) []*Deck {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Deck
	for _, d := range s.decks {
		if d.UserID == userID {
			result = append(result, d)
		}
	}
	return result
}

func (s *Store) GetDeck(id, userID string) *Deck {
	s.mu.RLock()
	defer s.mu.RUnlock()

	d, ok := s.decks[id]
	if !ok || d.UserID != userID {
		return nil
	}
	return d
}

func (s *Store) UpdateDeck(id, userID, name, color string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.decks[id]
	if !ok || d.UserID != userID {
		return ErrNotFound
	}
	d.Name = name
	d.Color = color
	d.UpdatedAt = time.Now()
	return s.save()
}

func (s *Store) DeleteDeck(id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.decks[id]
	if !ok || d.UserID != userID {
		return ErrNotFound
	}
	delete(s.decks, id)
	// Delete all cards in this deck
	for cid, c := range s.cards {
		if c.DeckID == id {
			delete(s.cards, cid)
		}
	}
	_ = d
	return s.save()
}

// --- Card Operations ---

func (s *Store) CreateCard(userID, deckID, front, back string) (*Card, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify deck belongs to user
	d, ok := s.decks[deckID]
	if !ok || d.UserID != userID {
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
	s.cards[card.ID] = card
	return card, s.save()
}

func (s *Store) GetCardsByDeck(deckID, userID string) []*Card {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Card
	for _, c := range s.cards {
		if c.DeckID == deckID && c.UserID == userID {
			result = append(result, c)
		}
	}
	return result
}

func (s *Store) GetCard(id, userID string) *Card {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.cards[id]
	if !ok || c.UserID != userID {
		return nil
	}
	return c
}

func (s *Store) UpdateCard(id, userID, front, back string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cards[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	c.Front = front
	c.Back = back
	c.UpdatedAt = time.Now()
	return s.save()
}

func (s *Store) DeleteCard(id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cards[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}
	delete(s.cards, id)
	_ = c
	return s.save()
}

// ReviewCard applies SM-2 spaced repetition algorithm
func (s *Store) ReviewCard(id, userID string, rating ReviewRating) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.cards[id]
	if !ok || c.UserID != userID {
		return ErrNotFound
	}

	q := int(rating) - 1 // convert 1-5 to 0-4 for SM-2

	if q >= 3 {
		// Correct response
		switch c.Repetition {
		case 0:
			c.Interval = 1
		case 1:
			c.Interval = 6
		default:
			c.Interval = int(float64(c.Interval) * c.Ease)
		}
		c.Repetition++
	} else {
		// Incorrect response - reset
		c.Repetition = 0
		c.Interval = 1
	}

	// Update ease factor
	c.Ease = c.Ease + (0.1 - float64(4-q)*(0.08+float64(4-q)*0.02))
	if c.Ease < 1.3 {
		c.Ease = 1.3
	}

	c.NextReview = time.Now().AddDate(0, 0, c.Interval)
	c.UpdatedAt = time.Now()

	return s.save()
}

// GetDueCards returns cards that are due for review
func (s *Store) GetDueCards(deckID, userID string) []*Card {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var result []*Card
	for _, c := range s.cards {
		if c.DeckID == deckID && c.UserID == userID && !c.NextReview.After(now) {
			result = append(result, c)
		}
	}
	return result
}

// GetDueCardCount returns the number of due cards per deck
func (s *Store) GetDueCardCount(deckID, userID string) int {
	return len(s.GetDueCards(deckID, userID))
}

// GetCardCount returns total cards in a deck
func (s *Store) GetCardCount(deckID, userID string) int {
	return len(s.GetCardsByDeck(deckID, userID))
}

// --- Import/Export ---

func (s *Store) ExportDecks(userID string, deckIDs []string) (*ExportData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	export := &ExportData{
		Version: "1.0",
	}

	for _, did := range deckIDs {
		d, ok := s.decks[did]
		if !ok || d.UserID != userID {
			continue
		}
		export.Decks = append(export.Decks, *d)
		for _, c := range s.cards {
			if c.DeckID == did && c.UserID == userID {
				export.Cards = append(export.Cards, *c)
			}
		}
	}
	return export, nil
}

func (s *Store) ImportDecks(userID string, data *ExportData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Map old deck IDs to new ones
	deckMap := make(map[string]string)

	for _, d := range data.Decks {
		newID := uuid.New().String()
		deckMap[d.ID] = newID
		newDeck := &Deck{
			ID:        newID,
			UserID:    userID,
			Name:      d.Name,
			Color:     d.Color,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		s.decks[newID] = newDeck
	}

	for _, c := range data.Cards {
		newDeckID, ok := deckMap[c.DeckID]
		if !ok {
			continue
		}
		newCard := &Card{
			ID:         uuid.New().String(),
			DeckID:     newDeckID,
			UserID:     userID,
			Front:      c.Front,
			Back:       c.Back,
			Ease:       2.5,
			Interval:   0,
			Repetition: 0,
			NextReview: time.Now(),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		s.cards[newCard.ID] = newCard
	}

	return s.save()
}

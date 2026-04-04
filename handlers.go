package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"time"
)

// App holds the application state
type App struct {
	store     *Store
	templates *template.Template
}

// NewApp creates a new application instance
func NewApp(store *Store) (*App, error) {
	funcMap := template.FuncMap{
		"cardCount": func(deckID, userID string) int {
			return store.GetCardCount(deckID, userID)
		},
		"dueCount": func(deckID, userID string) int {
			return store.GetDueCardCount(deckID, userID)
		},
		"ratingLabel": func(r int) string {
			labels := map[int]string{
				1: "Keine Ahnung",
				2: "Schwer",
				3: "Okay",
				4: "Gut",
				5: "Perfekt",
			}
			return labels[r]
		},
		"ratingEmoji": func(r int) string {
			emojis := map[int]string{
				1: "😵",
				2: "😰",
				3: "🤔",
				4: "😊",
				5: "🌟",
			}
			return emojis[r]
		},
		"timeFormat": func(t time.Time) string {
			return t.Format("02.01.2006")
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"formatCard": FormatCardText,
		"percent": func(part, total int) int {
			if total == 0 {
				return 0
			}
			return part * 100 / total
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseGlob("templates/pages/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	return &App{store: store, templates: tmpl}, nil
}

// render executes a template with common data
func (a *App) render(w http.ResponseWriter, tmpl string, data map[string]interface{}) {
	if err := a.templates.ExecuteTemplate(w, tmpl, data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// getUser returns the current user from the session cookie
func (a *App) getUser(r *http.Request) *User {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}
	return a.store.GetUserBySession(cookie.Value)
}

// requireAuth redirects to login if not authenticated
func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.getUser(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// --- Auth Handlers ---

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		msg := r.URL.Query().Get("msg")
		a.render(w, "login.html", map[string]interface{}{
			"Message": msg,
		})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		a.render(w, "login.html", map[string]interface{}{
			"Error": "Bitte fülle alle Felder aus.",
		})
		return
	}

	user, err := a.store.AuthenticateUser(username, password)
	if err != nil {
		a.render(w, "login.html", map[string]interface{}{
			"Error": "Ungültiger Benutzername oder Passwort.",
		})
		return
	}

	token := a.store.CreateSession(user.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 30, // 30 days
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.render(w, "register.html", map[string]interface{}{})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	if username == "" || password == "" {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Bitte fülle alle Felder aus.",
		})
		return
	}

	if len(username) < 3 {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Benutzername muss mindestens 3 Zeichen haben.",
		})
		return
	}

	if len(password) < 6 {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Passwort muss mindestens 6 Zeichen haben.",
		})
		return
	}

	if password != passwordConfirm {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Passwörter stimmen nicht überein.",
		})
		return
	}

	_, err := a.store.CreateUser(username, password)
	if err == ErrUserExists {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Benutzername ist bereits vergeben.",
		})
		return
	}
	if err != nil {
		a.render(w, "register.html", map[string]interface{}{
			"Error": "Ein Fehler ist aufgetreten.",
		})
		return
	}

	http.Redirect(w, r, "/login?msg=Account+erstellt!+Bitte+einloggen.", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		a.store.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Dashboard ---

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	decks := a.store.GetDecksByUser(user.ID)

	sort.Slice(decks, func(i, j int) bool {
		return decks[i].UpdatedAt.After(decks[j].UpdatedAt)
	})

	a.render(w, "dashboard.html", map[string]interface{}{
		"User":  user,
		"Decks": decks,
	})
}

// --- Deck Handlers ---

func (a *App) handleDeckCreate(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)

	if r.Method == http.MethodGet {
		a.render(w, "deck_form.html", map[string]interface{}{
			"User":   user,
			"IsEdit": false,
		})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	color := r.FormValue("color")
	if color == "" {
		color = "#6c5ce7"
	}

	if name == "" {
		a.render(w, "deck_form.html", map[string]interface{}{
			"User":   user,
			"IsEdit": false,
			"Error":  "Bitte gib einen Namen ein.",
		})
		return
	}

	_, err := a.store.CreateDeck(user.ID, name, color)
	if err != nil {
		a.render(w, "deck_form.html", map[string]interface{}{
			"User":   user,
			"IsEdit": false,
			"Error":  "Fehler beim Erstellen.",
		})
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleDeckEdit(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/deck/edit/")

	deck := a.store.GetDeck(deckID, user.ID)
	if deck == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet {
		a.render(w, "deck_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"IsEdit": true,
		})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	color := r.FormValue("color")

	if name == "" {
		a.render(w, "deck_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"IsEdit": true,
			"Error":  "Bitte gib einen Namen ein.",
		})
		return
	}

	a.store.UpdateDeck(deckID, user.ID, name, color)
	http.Redirect(w, r, "/deck/"+deckID, http.StatusSeeOther)
}

func (a *App) handleDeckDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/deck/delete/")
	a.store.DeleteDeck(deckID, user.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleDeckReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/deck/reset/")
	if a.store.GetDeck(deckID, user.ID) == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.store.ResetDeck(deckID, user.ID)
	http.Redirect(w, r, "/deck/"+deckID, http.StatusSeeOther)
}

func (a *App) handleDeckView(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/deck/")

	// Avoid matching sub-paths
	if strings.Contains(deckID, "/") {
		http.NotFound(w, r)
		return
	}

	deck := a.store.GetDeck(deckID, user.ID)
	if deck == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	cards := a.store.GetCardsByDeck(deckID, user.ID)
	sort.Slice(cards, func(i, j int) bool {
		return cards[i].CreatedAt.After(cards[j].CreatedAt)
	})

	a.render(w, "deck_view.html", map[string]interface{}{
		"User":  user,
		"Deck":  deck,
		"Cards": cards,
	})
}

// --- Card Handlers ---

func (a *App) handleCardCreate(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/card/create/")

	deck := a.store.GetDeck(deckID, user.ID)
	if deck == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet {
		a.render(w, "card_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"IsEdit": false,
		})
		return
	}

	front := strings.TrimSpace(r.FormValue("front"))
	back := strings.TrimSpace(r.FormValue("back"))

	if front == "" || back == "" {
		a.render(w, "card_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"IsEdit": false,
			"Error":  "Bitte fülle beide Seiten aus.",
		})
		return
	}

	a.store.CreateCard(user.ID, deckID, front, back)
	http.Redirect(w, r, "/deck/"+deckID, http.StatusSeeOther)
}

func (a *App) handleCardEdit(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	cardID := strings.TrimPrefix(r.URL.Path, "/card/edit/")

	card := a.store.GetCard(cardID, user.ID)
	if card == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	deck := a.store.GetDeck(card.DeckID, user.ID)

	if r.Method == http.MethodGet {
		a.render(w, "card_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"Card":   card,
			"IsEdit": true,
		})
		return
	}

	front := strings.TrimSpace(r.FormValue("front"))
	back := strings.TrimSpace(r.FormValue("back"))

	if front == "" || back == "" {
		a.render(w, "card_form.html", map[string]interface{}{
			"User":   user,
			"Deck":   deck,
			"Card":   card,
			"IsEdit": true,
			"Error":  "Bitte fülle beide Seiten aus.",
		})
		return
	}

	a.store.UpdateCard(cardID, user.ID, front, back)
	http.Redirect(w, r, "/deck/"+card.DeckID, http.StatusSeeOther)
}

func (a *App) handleCardDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := a.getUser(r)
	cardID := strings.TrimPrefix(r.URL.Path, "/card/delete/")

	card := a.store.GetCard(cardID, user.ID)
	if card == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	deckID := card.DeckID
	a.store.DeleteCard(cardID, user.ID)
	http.Redirect(w, r, "/deck/"+deckID, http.StatusSeeOther)
}

// --- Study Mode ---

// parseQueue splits a comma-separated queue string into a slice of IDs, ignoring empty entries.
func parseQueue(q string) []string {
	if q == "" {
		return nil
	}
	parts := strings.Split(q, ",")
	result := parts[:0]
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (a *App) handleStudy(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/study/")

	deck := a.store.GetDeck(deckID, user.ID)
	if deck == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	allMode := r.URL.Query().Get("all") == "1"
	q := r.URL.Query()

	// --- Session done ---
	if q.Get("done") == "1" {
		var r1, r2, r3, r4, r5, total int
		fmt.Sscanf(q.Get("r1"), "%d", &r1)
		fmt.Sscanf(q.Get("r2"), "%d", &r2)
		fmt.Sscanf(q.Get("r3"), "%d", &r3)
		fmt.Sscanf(q.Get("r4"), "%d", &r4)
		fmt.Sscanf(q.Get("r5"), "%d", &r5)
		fmt.Sscanf(q.Get("t"), "%d", &total)
		a.render(w, "study_done.html", map[string]interface{}{
			"User":     user,
			"Deck":     deck,
			"HasCards": a.store.GetCardCount(deckID, user.ID) > 0,
			"Total":    total,
			"R1":       r1, "R2": r2, "R3": r3, "R4": r4, "R5": r5,
		})
		return
	}

	// --- Initial visit: build shuffled queue ---
	if q.Get("card") == "" {
		var cards []*Card
		if allMode {
			cards = a.store.GetCardsByDeck(deckID, user.ID)
			rand.Shuffle(len(cards), func(i, j int) { cards[i], cards[j] = cards[j], cards[i] })
		} else {
			cards = a.store.GetDueCards(deckID, user.ID) // already shuffled
		}
		if len(cards) == 0 {
			a.render(w, "study_done.html", map[string]interface{}{
				"User":     user,
				"Deck":     deck,
				"HasCards": a.store.GetCardCount(deckID, user.ID) > 0,
				"Total":    0,
				"R1":       0, "R2": 0, "R3": 0, "R4": 0, "R5": 0,
			})
			return
		}
		ids := make([]string, len(cards))
		for i, c := range cards {
			ids[i] = c.ID
		}
		rest := strings.Join(ids[1:], ",")
		redirect := fmt.Sprintf("/study/%s?card=%s&q=%s&t=%d&r1=0&r2=0&r3=0&r4=0&r5=0",
			deckID, ids[0], rest, len(cards))
		if allMode {
			redirect += "&all=1"
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	// --- Show card ---
	var r1, r2, r3, r4, r5, total int
	fmt.Sscanf(q.Get("r1"), "%d", &r1)
	fmt.Sscanf(q.Get("r2"), "%d", &r2)
	fmt.Sscanf(q.Get("r3"), "%d", &r3)
	fmt.Sscanf(q.Get("r4"), "%d", &r4)
	fmt.Sscanf(q.Get("r5"), "%d", &r5)
	fmt.Sscanf(q.Get("t"), "%d", &total)

	queueIDs := parseQueue(q.Get("q"))
	remaining := 1 + len(queueIDs)

	card := a.store.GetCard(q.Get("card"), user.ID)
	if card == nil {
		http.Redirect(w, r, "/study/"+deckID, http.StatusSeeOther)
		return
	}

	done := total - remaining
	if done < 0 {
		done = 0
	}
	progressPct := 0
	if total > 0 {
		progressPct = done * 100 / total
	}

	a.render(w, "study.html", map[string]interface{}{
		"User":        user,
		"Deck":        deck,
		"Card":        card,
		"Reveal":      q.Get("reveal") == "1",
		"Remaining":   remaining,
		"Total":       total,
		"Done":        done,
		"ProgressPct": progressPct,
		"AllMode":     allMode,
		"Q":           q.Get("q"),
		"T":           q.Get("t"),
		"R1":          r1, "R2": r2, "R3": r3, "R4": r4, "R5": r5,
	})
}

// handleStudyMix starts a mixed study session with multiple decks.
// It accepts POST with checkboxes named "decks" or GET with ?decks=id1,id2,...
func (a *App) handleStudyMix(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)

	var deckIDs []string
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		deckIDs = r.Form["decks"]
	} else {
		ids := r.URL.Query().Get("decks")
		if ids != "" {
			deckIDs = strings.Split(ids, ",")
		}
	}

	// validate and deduplicate
	seen := map[string]bool{}
	valid := deckIDs[:0]
	for _, did := range deckIDs {
		did = strings.TrimSpace(did)
		if did == "" || seen[did] {
			continue
		}
		if d := a.store.GetDeck(did, user.ID); d != nil {
			valid = append(valid, did)
			seen[did] = true
		}
	}
	if len(valid) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if len(valid) == 1 {
		// Single deck: redirect to normal study
		http.Redirect(w, r, "/study/"+valid[0], http.StatusSeeOther)
		return
	}

	allMode := r.URL.Query().Get("all") == "1" || r.FormValue("all") == "1"

	var cards []*Card
	if allMode {
		cards = a.store.GetCardsByDecks(valid, user.ID)
	} else {
		cards = a.store.GetDueCardsMulti(valid, user.ID)
	}

	// Use a synthetic deck name for the done-page
	// Reuse handleStudy logic via /study/mix?card=...&decks=...
	decksParam := strings.Join(valid, ",")

	if len(cards) == 0 {
		// Build deck names for the done page
		var names []string
		for _, did := range valid {
			if d := a.store.GetDeck(did, user.ID); d != nil {
				names = append(names, d.Name)
			}
		}
		a.render(w, "study_done.html", map[string]interface{}{
			"User":     user,
			"Deck":     &Deck{ID: "mix", Name: strings.Join(names, " + ")},
			"HasCards": true,
			"MixDecks": decksParam,
			"Total":    0,
			"R1":       0, "R2": 0, "R3": 0, "R4": 0, "R5": 0,
		})
		return
	}

	ids := make([]string, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	rest := strings.Join(ids[1:], ",")
	redirect := fmt.Sprintf("/study/mix?card=%s&q=%s&t=%d&r1=0&r2=0&r3=0&r4=0&r5=0&decks=%s",
		ids[0], rest, len(cards), decksParam)
	if allMode {
		redirect += "&all=1"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// handleStudyMixShow handles GET /study/mix?card=...&q=...
func (a *App) handleStudyMixShow(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	q := r.URL.Query()
	decksParam := q.Get("decks")
	deckIDs := strings.Split(decksParam, ",")

	// Build display name
	var names []string
	for _, did := range deckIDs {
		if d := a.store.GetDeck(did, user.ID); d != nil {
			names = append(names, d.Name)
		}
	}
	mixName := strings.Join(names, " + ")
	fakeDeck := &Deck{ID: "mix", Name: mixName}

	allMode := q.Get("all") == "1"

	if q.Get("done") == "1" {
		var r1, r2, r3, r4, r5, total int
		fmt.Sscanf(q.Get("r1"), "%d", &r1)
		fmt.Sscanf(q.Get("r2"), "%d", &r2)
		fmt.Sscanf(q.Get("r3"), "%d", &r3)
		fmt.Sscanf(q.Get("r4"), "%d", &r4)
		fmt.Sscanf(q.Get("r5"), "%d", &r5)
		fmt.Sscanf(q.Get("t"), "%d", &total)
		a.render(w, "study_done.html", map[string]interface{}{
			"User":     user,
			"Deck":     fakeDeck,
			"HasCards": true,
			"MixDecks": decksParam,
			"Total":    total,
			"R1":       r1, "R2": r2, "R3": r3, "R4": r4, "R5": r5,
		})
		return
	}

	var r1, r2, r3, r4, r5, total int
	fmt.Sscanf(q.Get("r1"), "%d", &r1)
	fmt.Sscanf(q.Get("r2"), "%d", &r2)
	fmt.Sscanf(q.Get("r3"), "%d", &r3)
	fmt.Sscanf(q.Get("r4"), "%d", &r4)
	fmt.Sscanf(q.Get("r5"), "%d", &r5)
	fmt.Sscanf(q.Get("t"), "%d", &total)

	queueIDs := parseQueue(q.Get("q"))
	remaining := 1 + len(queueIDs)

	card := a.store.GetCard(q.Get("card"), user.ID)
	if card == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	done := total - remaining
	if done < 0 {
		done = 0
	}
	progressPct := 0
	if total > 0 {
		progressPct = done * 100 / total
	}

	a.render(w, "study.html", map[string]interface{}{
		"User":        user,
		"Deck":        fakeDeck,
		"Card":        card,
		"Reveal":      q.Get("reveal") == "1",
		"Remaining":   remaining,
		"Total":       total,
		"Done":        done,
		"ProgressPct": progressPct,
		"AllMode":     allMode,
		"Q":           q.Get("q"),
		"T":           q.Get("t"),
		"MixDecks":    decksParam,
		"R1":          r1, "R2": r2, "R3": r3, "R4": r4, "R5": r5,
	})
}

func (a *App) handleStudyRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	user := a.getUser(r)
	cardID := r.FormValue("card_id")
	deckID := r.FormValue("deck_id")
	ratingStr := r.FormValue("rating")

	var rating int
	fmt.Sscanf(ratingStr, "%d", &rating)
	if rating < 1 || rating > 5 {
		rating = 3
	}

	// Parse current queue and stats
	queue := parseQueue(r.FormValue("q"))
	var r1, r2, r3, r4, r5, total int
	fmt.Sscanf(r.FormValue("r1"), "%d", &r1)
	fmt.Sscanf(r.FormValue("r2"), "%d", &r2)
	fmt.Sscanf(r.FormValue("r3"), "%d", &r3)
	fmt.Sscanf(r.FormValue("r4"), "%d", &r4)
	fmt.Sscanf(r.FormValue("r5"), "%d", &r5)
	fmt.Sscanf(r.FormValue("t"), "%d", &total)
	allMode := r.FormValue("all") == "1"

	switch rating {
	case 1:
		r1++
	case 2:
		r2++
	case 3:
		r3++
	case 4:
		r4++
	case 5:
		r5++
	}

	// Build new queue
	var newQueue []string
	switch rating {
	case 1: // Nochmal: re-insert soon (after ~3 cards)
		insertPos := 3
		if insertPos > len(queue) {
			insertPos = len(queue)
		}
		newQueue = make([]string, 0, len(queue)+1)
		newQueue = append(newQueue, queue[:insertPos]...)
		newQueue = append(newQueue, cardID)
		newQueue = append(newQueue, queue[insertPos:]...)
		total++
	case 2: // Schwer: re-insert at end of session
		newQueue = append(queue, cardID)
		total++
	default: // Okay/Gut/Einfach: apply SM-2, remove from queue
		a.store.ReviewCard(cardID, user.ID, ReviewRating(rating))
		newQueue = queue
	}

	// Session done?
	mixDecks := r.FormValue("mix_decks")
	if len(newQueue) == 0 {
		var redirect string
		if mixDecks != "" {
			redirect = fmt.Sprintf("/study/mix?done=1&t=%d&r1=%d&r2=%d&r3=%d&r4=%d&r5=%d&decks=%s",
				total, r1, r2, r3, r4, r5, mixDecks)
		} else {
			redirect = fmt.Sprintf("/study/%s?done=1&t=%d&r1=%d&r2=%d&r3=%d&r4=%d&r5=%d",
				deckID, total, r1, r2, r3, r4, r5)
		}
		if allMode {
			redirect += "&all=1"
		}
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	// Next card
	next := newQueue[0]
	rest := strings.Join(newQueue[1:], ",")
	var redirect string
	if mixDecks != "" {
		redirect = fmt.Sprintf("/study/mix?card=%s&q=%s&t=%d&r1=%d&r2=%d&r3=%d&r4=%d&r5=%d&decks=%s",
			next, rest, total, r1, r2, r3, r4, r5, mixDecks)
	} else {
		redirect = fmt.Sprintf("/study/%s?card=%s&q=%s&t=%d&r1=%d&r2=%d&r3=%d&r4=%d&r5=%d",
			deckID, next, rest, total, r1, r2, r3, r4, r5)
	}
	if allMode {
		redirect += "&all=1"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// --- Import/Export ---

func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/export/")

	var deckIDs []string
	if deckID == "all" {
		decks := a.store.GetDecksByUser(user.ID)
		for _, d := range decks {
			deckIDs = append(deckIDs, d.ID)
		}
	} else {
		deckIDs = []string{deckID}
	}

	data, err := a.store.ExportDecks(user.ID, deckIDs)
	if err != nil {
		http.Error(w, "Export fehlgeschlagen", http.StatusInternalServerError)
		return
	}

	jsonData, _ := json.MarshalIndent(data, "", "  ")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=myindexcards_export.json")
	w.Write(jsonData)
}

func (a *App) handleImport(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)

	if r.Method == http.MethodGet {
		a.render(w, "import.html", map[string]interface{}{
			"User": user,
		})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		a.render(w, "import.html", map[string]interface{}{
			"User":  user,
			"Error": "Bitte wähle eine Datei aus.",
		})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 10<<20)) // 10MB limit
	if err != nil {
		a.render(w, "import.html", map[string]interface{}{
			"User":  user,
			"Error": "Fehler beim Lesen der Datei.",
		})
		return
	}

	var exportData ExportData
	if err := json.Unmarshal(data, &exportData); err != nil {
		a.render(w, "import.html", map[string]interface{}{
			"User":  user,
			"Error": "Ungültiges Dateiformat. Bitte verwende eine MYIndexCards JSON-Datei.",
		})
		return
	}

	if err := a.store.ImportDecks(user.ID, &exportData); err != nil {
		a.render(w, "import.html", map[string]interface{}{
			"User":  user,
			"Error": "Fehler beim Importieren.",
		})
		return
	}

	http.Redirect(w, r, "/?msg=Import+erfolgreich!", http.StatusSeeOther)
}

// --- Router ---

func (a *App) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Auth routes
	mux.HandleFunc("/login", a.handleLogin)
	mux.HandleFunc("/register", a.handleRegister)
	mux.HandleFunc("/logout", a.handleLogout)

	// Protected routes
	mux.HandleFunc("/", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		a.handleDashboard(w, r)
	}))

	// Deck routes
	mux.HandleFunc("/deck/create", a.requireAuth(a.handleDeckCreate))
	mux.HandleFunc("/deck/edit/", a.requireAuth(a.handleDeckEdit))
	mux.HandleFunc("/deck/delete/", a.requireAuth(a.handleDeckDelete))
	mux.HandleFunc("/deck/reset/", a.requireAuth(a.handleDeckReset))
	mux.HandleFunc("/deck/", a.requireAuth(a.handleDeckView))

	// Card routes
	mux.HandleFunc("/card/create/", a.requireAuth(a.handleCardCreate))
	mux.HandleFunc("/card/edit/", a.requireAuth(a.handleCardEdit))
	mux.HandleFunc("/card/delete/", a.requireAuth(a.handleCardDelete))

	// Study routes
	mux.HandleFunc("/study/mix", a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.URL.Query().Get("card") == "" {
			a.handleStudyMix(w, r)
		} else {
			a.handleStudyMixShow(w, r)
		}
	}))
	mux.HandleFunc("/study/", a.requireAuth(a.handleStudy))
	mux.HandleFunc("/study/rate", a.requireAuth(a.handleStudyRate))

	// Import/Export
	mux.HandleFunc("/export/", a.requireAuth(a.handleExport))
	mux.HandleFunc("/import", a.requireAuth(a.handleImport))

	return mux
}

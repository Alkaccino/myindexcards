package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
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

func (a *App) handleStudy(w http.ResponseWriter, r *http.Request) {
	user := a.getUser(r)
	deckID := strings.TrimPrefix(r.URL.Path, "/study/")

	deck := a.store.GetDeck(deckID, user.ID)
	if deck == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	allMode := r.URL.Query().Get("all") == "1"

	var studyCards []*Card
	totalCards := 0
	if allMode {
		since := r.URL.Query().Get("since")
		if since == "" {
			http.Redirect(w, r, fmt.Sprintf("/study/%s?all=1&since=%d&r1=0&r2=0&r3=0&r4=0&r5=0", deckID, time.Now().UnixMilli()), http.StatusSeeOther)
			return
		}
		var sinceMs int64
		fmt.Sscanf(since, "%d", &sinceMs)
		sinceTime := time.UnixMilli(sinceMs)
		allCards := a.store.GetCardsByDeck(deckID, user.ID)
		totalCards = len(allCards)
		for _, c := range allCards {
			if c.UpdatedAt.Before(sinceTime) || c.UpdatedAt.Equal(sinceTime) {
				studyCards = append(studyCards, c)
			}
		}
	} else {
		studyCards = a.store.GetDueCards(deckID, user.ID)
		// For due-mode, initialize session total on first visit
		tParam := r.URL.Query().Get("t")
		if tParam == "" && len(studyCards) > 0 {
			http.Redirect(w, r, fmt.Sprintf("/study/%s?t=%d&r1=0&r2=0&r3=0&r4=0&r5=0", deckID, len(studyCards)), http.StatusSeeOther)
			return
		}
		fmt.Sscanf(tParam, "%d", &totalCards)
		if totalCards < len(studyCards) {
			totalCards = len(studyCards)
		}
	}

	// Parse session rating counters
	var r1, r2, r3, r4, r5 int
	fmt.Sscanf(r.URL.Query().Get("r1"), "%d", &r1)
	fmt.Sscanf(r.URL.Query().Get("r2"), "%d", &r2)
	fmt.Sscanf(r.URL.Query().Get("r3"), "%d", &r3)
	fmt.Sscanf(r.URL.Query().Get("r4"), "%d", &r4)
	fmt.Sscanf(r.URL.Query().Get("r5"), "%d", &r5)
	reviewed := r1 + r2 + r3 + r4 + r5

	if len(studyCards) == 0 {
		a.render(w, "study_done.html", map[string]interface{}{
			"User":     user,
			"Deck":     deck,
			"HasCards": a.store.GetCardCount(deckID, user.ID) > 0,
			"Total":    reviewed,
			"R1":       r1,
			"R2":       r2,
			"R3":       r3,
			"R4":       r4,
			"R5":       r5,
		})
		return
	}

	reveal := r.URL.Query().Get("reveal") == "1"
	cardID := r.URL.Query().Get("card")
	var card *Card
	if cardID != "" {
		card = a.store.GetCard(cardID, user.ID)
	}
	if card == nil {
		card = studyCards[0]
		reveal = false
	}

	done := totalCards - len(studyCards)
	progressPct := 0
	if totalCards > 0 {
		progressPct = done * 100 / totalCards
	}

	a.render(w, "study.html", map[string]interface{}{
		"User":        user,
		"Deck":        deck,
		"Card":        card,
		"Reveal":      reveal,
		"Remaining":   len(studyCards),
		"Total":       totalCards,
		"Done":        done,
		"ProgressPct": progressPct,
		"AllMode":     allMode,
		"Since":       r.URL.Query().Get("since"),
		"T":           r.URL.Query().Get("t"),
		"R1":          r1,
		"R2":          r2,
		"R3":          r3,
		"R4":          r4,
		"R5":          r5,
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

	// Parse current session rating counters
	var r1, r2, r3, r4, r5 int
	fmt.Sscanf(r.FormValue("r1"), "%d", &r1)
	fmt.Sscanf(r.FormValue("r2"), "%d", &r2)
	fmt.Sscanf(r.FormValue("r3"), "%d", &r3)
	fmt.Sscanf(r.FormValue("r4"), "%d", &r4)
	fmt.Sscanf(r.FormValue("r5"), "%d", &r5)

	// Increment the counter for this rating
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

	// Ratings 4-5: card is learned, apply SM-2 and remove from queue
	// Ratings 1-3: card stays in queue (no SM-2 update, no timestamp change)
	if rating >= 4 {
		a.store.ReviewCard(cardID, user.ID, ReviewRating(rating))
	}

	redirect := fmt.Sprintf("/study/%s?r1=%d&r2=%d&r3=%d&r4=%d&r5=%d", deckID, r1, r2, r3, r4, r5)
	if r.FormValue("all") == "1" {
		redirect += "&all=1&since=" + r.FormValue("since")
	} else if t := r.FormValue("t"); t != "" {
		redirect += "&t=" + t
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
	mux.HandleFunc("/deck/", a.requireAuth(a.handleDeckView))

	// Card routes
	mux.HandleFunc("/card/create/", a.requireAuth(a.handleCardCreate))
	mux.HandleFunc("/card/edit/", a.requireAuth(a.handleCardEdit))
	mux.HandleFunc("/card/delete/", a.requireAuth(a.handleCardDelete))

	// Study routes
	mux.HandleFunc("/study/", a.requireAuth(a.handleStudy))
	mux.HandleFunc("/study/rate", a.requireAuth(a.handleStudyRate))

	// Import/Export
	mux.HandleFunc("/export/", a.requireAuth(a.handleExport))
	mux.HandleFunc("/import", a.requireAuth(a.handleImport))

	return mux
}

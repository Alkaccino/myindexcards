package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://indexcards:indexcards@localhost:5432/indexcards?sslmode=disable"
	}

	store, err := NewStore(dsn)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	app, err := NewApp(store)
	if err != nil {
		log.Fatalf("Failed to initialize app: %v", err)
	}

	mux := app.SetupRoutes()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("🃏 MYIndexCards läuft auf http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

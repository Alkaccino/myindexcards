package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	store, err := NewStore("data")
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	app, err := NewApp(store)
	if err != nil {
		log.Fatalf("Failed to initialize app: %v", err)
	}

	mux := app.SetupRoutes()

	port := 8080
	fmt.Printf("🃏 MYIndexCards läuft auf http://localhost:%d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), mux))
}

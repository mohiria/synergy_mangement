package main

import (
	"log"
	"net/http"
	"os"

	"synergy/server/internal/api"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
	handler := api.HandlerFromMuxWithBaseURL(api.NewServer(), mux, "/api/v1")

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

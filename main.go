package main

import (
	"log"
	"os"

	"github.com/ravener/discord-oauth2"
	"golang.org/x/oauth2"
)

func main() {
	// disable ssl mode for lib/pq
	conStr := "postgresql://gobank:gobank@db/gobank?sslmode=disable"
	store, err := NewPostgresStore(conStr)
	if err != nil {
		log.Fatal(err)
	}

	if err := store.Init(); err != nil {
		log.Fatal(err)
	}

	clientId := os.Getenv("CLIENT_ID")
	secret := os.Getenv("CLIENT_SECRET")
	auth := &oauth2.Config{
		RedirectURL:  "http://localhost:3000/auth/callback",
		ClientID:     clientId,
		ClientSecret: secret,
		Scopes:       []string{discord.ScopeIdentify},
		Endpoint:     discord.Endpoint,
	}

	server := NewApiService(":3000", store, auth)
	server.Run()
}

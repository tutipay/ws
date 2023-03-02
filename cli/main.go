package main

import (
	"log"
	"net/http"

	chat "github.com/tutipay/ws"
)

func serveHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	db, err := chat.OpenDb("test.db")
	if err != nil {
		log.Fatalf("the data is null: %v", err)
	}
	hub := chat.NewHub(db)
	go hub.Run()
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHome)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWs(hub, w, r)
	})

	// This is only for testing it's not used in production
	mux.HandleFunc("/submitContacts", func(w http.ResponseWriter, r *http.Request) {
		chat.SubmitContacts("0123456789", "test.db", w, r)
	})

	log.Fatal(http.ListenAndServe(":6446", mux))
}

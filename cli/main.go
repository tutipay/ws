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

	http.ListenAndServe(":6446", mux)
}

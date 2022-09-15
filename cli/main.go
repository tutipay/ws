package main

import (
	"net/http"

	"github.com/jmoiron/sqlx"
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
	hub := chat.NewHub()
	go hub.Run()
	msg, _ := chat.NewMessage(&sqlx.DB{})
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHome)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWs(hub, w, r)
	})
	mux.HandleFunc("/chats", func(w http.ResponseWriter, r *http.Request) {
		chat.PreviousMessages(msg, w, r)
	})
	http.ListenAndServe(":8081", mux)
}

package main

import (
	"log"
	"net/http"
	"sync"

	chat "github.com/tutipay/ws"
)

var m sync.Mutex

func serveHome(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path == "/69" {
		http.ServeFile(w, r, "69.html")
		return
	}

	if r.URL.Path == "/420" {
		http.ServeFile(w, r, "420.html")
		return
	}

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
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHome)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWs(hub, w, r)
	})
	http.ListenAndServe(":8081", mux)
}

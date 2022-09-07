package main

import (
	"net/http"

	"github.com/tutipay/chat"
)

func main() {
	hub := chat.NewHub()
	go hub.run()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWs(hub, w, r)
	})
	http.ListenAndServe(":8081", mux)
}

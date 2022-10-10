package chat

import (
	"log"
	"net/http"
)

// serveWs handles websocket requests from the peer. The hub needs to be populated
// with a *sqlx.DB reference, since we will need to store messages
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	// We can change this to JSON instead
	clientID := r.URL.Query().Get("clientID")

	// Client should have a reference to db, since this is the only way we can inject
	// the db down there.
	client := &Client{db: hub.db, ID: clientID, hub: hub, conn: conn, send: make(chan *Message, 256)}
	client.hub.register <- client

	client.hub.clients[clientID] = client

	// Sending messages that were sent to the client when they were offline
	client.PreviousMessages()

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

package chat

import (
	"encoding/json"
	"log"
	"net/http"
)

// serveWs handles websocket requests from the peer. The hub needs to be populated
// with a *sqlx.DB reference, since we will need to store messages
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ServeWs upgrader error: %v", err)
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

// SubmitContacts
func SubmitContacts(currentUser string, w http.ResponseWriter, r *http.Request) {
	db, err := OpenDb("test.db")
	if err != nil {
		log.Printf("Could not open db: %v", err)
		return
	}

	decoder := json.NewDecoder(r.Body)

	var contacts []ContactsRequest
	if err := decoder.Decode(&contacts); err != nil {
		log.Printf("Error in parsing JSON: %v", err)
		return
	}

	addContactsToDB(currentUser, contacts, db)

	w.Write(marshal(contacts))
}

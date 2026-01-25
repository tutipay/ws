package chat

import (
	"encoding/json"
	"log"
	"net/http"
)

// serveWs handles websocket requests from the peer. The hub needs to be populated
// with a *sqlx.DB reference, since we will need to store messages
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	// We can change this to JSON instead
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		http.Error(w, "clientID is required", http.StatusBadRequest)
		return
	}

	conn, err := hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ServeWs upgrader error: %v", err)
		return
	}

	// Client should have a reference to db, since this is the only way we can inject
	// the db down there.
	client := &Client{
		db:   hub.db,
		ID:   clientID,
		hub:  hub,
		conn: conn,
		send: make(chan outbound, hub.cfg.ClientSendBuffer),
		done: make(chan struct{}),
	}
	if ok := client.hub.registerClient(client); !ok {
		conn.Close()
		return
	}

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()

	// Sending messages that were sent to the client when they were offline
	client.PreviousMessages()

	// This will broadcast that this client is online to all users who have this client as a contact
	client.NotifyStatus("online")
}

// SubmitContacts
func SubmitContacts(currentUser string, dbPath string, w http.ResponseWriter, r *http.Request) {
	db, err := OpenDb(dbPath)
	if err != nil {
		log.Printf("Could not open db: %v", err)
		http.Error(w, "could not open db", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	decoder := json.NewDecoder(r.Body)

	var contacts []ContactsRequest
	if err := decoder.Decode(&contacts); err != nil {
		log.Printf("Error in parsing JSON: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	areUsers, err := addContactsToDB(currentUser, contacts, db)
	if err != nil {
		log.Printf("Error inserting contacts: %v", err)
		http.Error(w, "could not save contacts", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write(marshal(areUsers))
}

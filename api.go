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

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

// PreviousMessages retrieves all messages that were sent to a senderID but they still didn't
// Read it.
// We will need to have a reference to a message instance (You can get one via: NewMessage()) and that will be populated
// with a *sqlx.DB instance
func PreviousMessages(msg Hub, w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		verr := validationError{Message: "Cliend ID is empty", Code: "empty_cliend_id"}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(marshal(verr))
		return
	}
	chats, err := getUnreadMessages(clientID, msg.db)
	if err != nil {
		verr := validationError{Message: "No previous unread messages", Code: "empty_queue"}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(marshal(verr))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(marshal(chats))
	updateStatus(clientID, msg.db)
}

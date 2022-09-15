package chat

import (
	"log"
	"net/http"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan *Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  make(chan *Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				// A message contains .to and .from fields in addition to the content
				// we could use that to match against the specific client ID we already have
				// and only send to the one of interest. There are other cases we need to check for:
				// - if the client.ID doesn't exist (not connected, or disconnected)
				// - handle delivery failures as well as storing the message itself in a database
				if message.To == client.ID {
					select {
					case client.send <- message:
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	// We can change this to JSON instead
	clientID := r.URL.Query().Get("clientID")

	client := &Client{ID: clientID, hub: hub, conn: conn, send: make(chan *Message, 256)}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

// PreviousMessages retrieves all messages that were sent to a senderID but they still didn't
// Read it.
func PreviousMessages(msg Message, w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("clientID")
	if clientID == "" {
		verr := validationError{Message: "Cliend ID is empty", Code: "empty_cliend_id"}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(marshal(verr))
		return
	}
	chats, err := msg.getUnreadMessages(clientID)
	if err != nil {
		verr := validationError{Message: "No previous unread messages", Code: "empty_queue"}
		w.WriteHeader(http.StatusBadRequest)
		w.Write(marshal(verr))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(marshal(chats))
	msg.readAll(clientID, msg.db)
}

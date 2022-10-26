package chat

import (
	"log"

	"github.com/jmoiron/sqlx"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[string]*Client // Mapping client IDs to client object

	// Inbound messages from the clients.
	broadcast chan *Message

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Database reference, we will need to have it down
	db *sqlx.DB
}

func NewHub(db *sqlx.DB) *Hub {
	return &Hub{
		broadcast:  make(chan *Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[string]*Client),
		db:         db,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client.ID] = client
		case client := <-h.unregister:
			log.Printf("client ID: %s got disconnected", client.ID)
			if _, ok := h.clients[client.ID]; ok {
				client.ShareStatus("offline")
				delete(h.clients, client.ID)
				close(client.send)
			}
		case message := <-h.broadcast:
			if client, ok := h.clients[message.To]; ok {
				// We don't need to check for the case of non-existing clients to store the message
				// in the database to send it to them later when they connect, because we store the
				// message in the database in `readPump` before we send it through the broadcast channel
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client.ID)
				}
			}
		}
	}
}

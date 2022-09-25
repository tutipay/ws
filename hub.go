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

func insert(msg Message, db *sqlx.DB) error {
	if _, err := db.NamedExec(`INSERT into chats("from", "to", "text") values(:from, :to, :text)`, msg); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func updateStatus(mobile string, db *sqlx.DB) error {
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "to" = $1`, mobile); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func getUnreadMessages(mobile string, db *sqlx.DB) ([]Message, error) {
	var chats []Message
	if err := db.Select(&chats, `SELECT * from chats where "to" = $1 and is_delivered = 0 order by id`, mobile); err != nil {
		return nil, err
	}
	return chats, nil
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
			if _, ok := h.clients[client.ID]; ok {
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

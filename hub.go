package chat

import (
	"log"

	"github.com/jmoiron/sqlx"
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
		clients:    make(map[*Client]bool),
		db:         db,
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
			// I feel like iterating through clients *everytime* we get a new message, is not the most
			// efficient way of doing this. We can do something like this:
			// client, ok := h.clients[message.To]
			// if !ok { // the client is not there
			// store the message in the database
			// insert(message)
			// }
			// client <- message // in this case, the client is there
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

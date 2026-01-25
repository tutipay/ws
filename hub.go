package chat

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[string]*Client // Mapping client IDs to client object

	// Inbound messages from the clients.
	broadcast chan *Message

	// Status updates for contacts.
	status chan statusUpdate

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Database reference, we will need to have it down
	db *sqlx.DB

	cfg HubConfig

	upgrader websocket.Upgrader

	done     chan struct{}
	stopOnce sync.Once
}

func NewHub(db *sqlx.DB) *Hub {
	return NewHubWithConfig(db, DefaultHubConfig())
}

func NewHubWithConfig(db *sqlx.DB, cfg HubConfig) *Hub {
	cfg = cfg.withDefaults()
	return &Hub{
		broadcast:  make(chan *Message, cfg.BroadcastBuffer),
		register:   make(chan *Client, cfg.RegisterBuffer),
		unregister: make(chan *Client, cfg.UnregisterBuffer),
		status:     make(chan statusUpdate, cfg.StatusBuffer),
		clients:    make(map[string]*Client),
		db:         db,
		cfg:        cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.ReadBufferSize,
			WriteBufferSize: cfg.WriteBufferSize,
			CheckOrigin:     cfg.CheckOrigin,
		},
		done: make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return
		case client := <-h.register:
			if existing, ok := h.clients[client.ID]; ok {
				existing.markReplaced()
				existing.close()
			}
			h.clients[client.ID] = client
		case client := <-h.unregister:
			log.Printf("client ID: %s got disconnected", client.ID)
			if existing, ok := h.clients[client.ID]; ok && existing == client {
				delete(h.clients, client.ID)
				client.close()
			}
		case message := <-h.broadcast:
			if client, ok := h.clients[message.To]; ok {
				// We don't need to check for the case of non-existing clients to store the message
				// in the database to send it to them later when they connect, because we store the
				// message in the database in `readPump` before we send it through the broadcast channel
				h.trySend(client, outbound{
					response:    Response{Messages: []Message{*message}},
					markReadIDs: []string{message.ID},
				})
			}
		case update := <-h.status:
			for _, contact := range update.contacts {
				if client, ok := h.clients[contact]; ok {
					h.trySend(client, outbound{
						response: Response{Status: StatusResponse{Mobile: update.from, ConnectionStatus: update.status}},
					})
				}
			}
		}
	}
}

func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.done)
	})
}

func (h *Hub) registerClient(client *Client) bool {
	select {
	case h.register <- client:
		return true
	case <-h.done:
		return false
	}
}

func (h *Hub) unregisterClient(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.done:
	}
}

func (h *Hub) broadcastMessage(message *Message) bool {
	select {
	case h.broadcast <- message:
		return true
	case <-h.done:
		return false
	}
}

func (h *Hub) queueStatus(update statusUpdate) bool {
	select {
	case h.status <- update:
		return true
	case <-h.done:
		return false
	}
}

func (h *Hub) trySend(client *Client, message outbound) {
	select {
	case <-client.done:
		delete(h.clients, client.ID)
		return
	default:
	}
	select {
	case client.send <- message:
	default:
		delete(h.clients, client.ID)
		client.close()
	}
}

type statusUpdate struct {
	from     string
	status   string
	contacts []string
}

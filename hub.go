package chat

import (
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

// Hub owns tenant-scoped live routing. ClientIdentity is the map key everywhere
// so equal user IDs in different tenants are independent connections.
type Hub struct {
	mu sync.RWMutex

	clients map[ClientIdentity]*Client
	typing  map[ClientIdentity]map[ClientIdentity]bool
	stopped bool

	broadcast chan *Message
	status    chan statusUpdate

	db  *sqlx.DB
	cfg HubConfig

	upgrader websocket.Upgrader

	done      chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
}

func NewHub(db *sqlx.DB) *Hub {
	return NewHubWithConfig(db, DefaultHubConfig())
}

func NewHubWithConfig(db *sqlx.DB, cfg HubConfig) *Hub {
	cfg = cfg.withDefaults()
	return &Hub{
		broadcast: make(chan *Message, cfg.BroadcastBuffer),
		status:    make(chan statusUpdate, cfg.StatusBuffer),
		clients:   make(map[ClientIdentity]*Client),
		typing:    make(map[ClientIdentity]map[ClientIdentity]bool),
		db:        db,
		cfg:       cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.ReadBufferSize,
			WriteBufferSize: cfg.WriteBufferSize,
			CheckOrigin:     cfg.CheckOrigin,
		},
		done: make(chan struct{}),
	}
}

func (h *Hub) Run() {
	h.startOnce.Do(func() {
		for range h.cfg.BroadcastWorkers {
			go h.broadcastWorker()
		}
		for range h.cfg.StatusWorkers {
			go h.statusWorker()
		}
	})
	<-h.done
}

func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.stopped = true
		clients := make([]*Client, 0, len(h.clients))
		for _, client := range h.clients {
			clients = append(clients, client)
		}
		h.mu.Unlock()
		close(h.done)
		for _, client := range clients {
			client.close()
		}
	})
}

func (h *Hub) registerClient(client *Client) bool {
	var replaced *Client
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return false
	}
	if existing, ok := h.clients[client.Identity]; ok {
		replaced = existing
	}
	h.clients[client.Identity] = client
	h.mu.Unlock()
	if replaced != nil {
		replaced.markReplaced()
		h.clearTypingFor(replaced.Identity)
		replaced.close()
	}
	return true
}

func (h *Hub) unregisterClient(client *Client) {
	h.removeClient(client)
}

func (h *Hub) broadcastMessage(message *Message) bool {
	select {
	case h.broadcast <- message:
		return true
	case <-h.done:
		return false
	default:
		h.handleBroadcast(message)
		return true
	}
}

func (h *Hub) queueStatus(update statusUpdate) bool {
	select {
	case h.status <- update:
		return true
	case <-h.done:
		return false
	default:
		h.handleStatus(update)
		return true
	}
}

type statusUpdate struct {
	from     ClientIdentity
	status   string
	contacts []ClientIdentity
}

func (h *Hub) broadcastWorker() {
	for {
		select {
		case <-h.done:
			return
		case message := <-h.broadcast:
			h.handleBroadcast(message)
		}
	}
}

func (h *Hub) handleBroadcast(message *Message) {
	if message == nil {
		return
	}
	if message.Type == FrameTypeTyping {
		h.handleTyping(message)
		return
	}
	h.clearTypingBetween(message.sender(), message.recipient(), true)
	if client := h.getClient(message.recipient()); client != nil {
		h.trySend(client, outbound{response: Response{Messages: []Message{*message}}})
	}
}

func (h *Hub) statusWorker() {
	for {
		select {
		case <-h.done:
			return
		case update := <-h.status:
			h.handleStatus(update)
		}
	}
}

func (h *Hub) handleStatus(update statusUpdate) {
	for _, contact := range update.contacts {
		if client := h.getClient(contact); client != nil {
			h.trySend(client, outbound{response: Response{Status: &StatusResponse{
				UserID: update.from.UserID, ConnectionStatus: update.status,
			}}})
		}
	}
}

func (h *Hub) persistMessage(message Message) (persistResult, error) {
	select {
	case <-h.done:
		return persistResult{}, errors.New("hub stopped")
	default:
		return insert(message, h.db)
	}
}

func (h *Hub) removeClient(client *Client) {
	removed := false
	h.mu.Lock()
	if existing, ok := h.clients[client.Identity]; ok && existing == client {
		delete(h.clients, client.Identity)
		removed = true
	}
	h.mu.Unlock()
	if removed {
		h.clearTypingFor(client.Identity)
	}
	client.close()
}

func (h *Hub) getClient(identity ClientIdentity) *Client {
	h.mu.RLock()
	client := h.clients[identity]
	h.mu.RUnlock()
	return client
}

func (h *Hub) handleTyping(message *Message) {
	from := message.sender()
	to := message.recipient()
	if from.validate() != nil || to.validate() != nil {
		return
	}
	isTyping := message.IsTyping != nil && *message.IsTyping
	h.setTyping(from, to, isTyping)
	if client := h.getClient(to); client != nil {
		h.trySendDrop(client, outbound{response: Response{Messages: []Message{*message}}})
	}
}

func (h *Hub) setTyping(from, to ClientIdentity, isTyping bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	targets, ok := h.typing[from]
	if !ok {
		if !isTyping {
			return
		}
		targets = make(map[ClientIdentity]bool)
		h.typing[from] = targets
	}
	if isTyping {
		targets[to] = true
		return
	}
	delete(targets, to)
	if len(targets) == 0 {
		delete(h.typing, from)
	}
}

func (h *Hub) clearTypingFor(from ClientIdentity) {
	h.mu.Lock()
	targets, ok := h.typing[from]
	if !ok {
		h.mu.Unlock()
		return
	}
	contacts := make([]ClientIdentity, 0, len(targets))
	for to := range targets {
		contacts = append(contacts, to)
	}
	delete(h.typing, from)
	h.mu.Unlock()

	for _, to := range contacts {
		if client := h.getClient(to); client != nil {
			isTyping := false
			message := Message{
				TenantID: from.TenantID, FromUserID: from.UserID, ToUserID: to.UserID,
				Type: FrameTypeTyping, IsTyping: &isTyping, Date: time.Now().UTC().Unix(),
			}
			h.trySendDrop(client, outbound{response: Response{Messages: []Message{message}}})
		}
	}
}

func (h *Hub) clearTypingBetween(from, to ClientIdentity, notify bool) {
	shouldNotify := false
	h.mu.Lock()
	if targets, ok := h.typing[from]; ok {
		if _, ok := targets[to]; ok {
			delete(targets, to)
			if len(targets) == 0 {
				delete(h.typing, from)
			}
			shouldNotify = notify
		}
	}
	h.mu.Unlock()
	if !shouldNotify {
		return
	}
	if client := h.getClient(to); client != nil {
		isTyping := false
		message := Message{
			TenantID: from.TenantID, FromUserID: from.UserID, ToUserID: to.UserID,
			Type: FrameTypeTyping, IsTyping: &isTyping, Date: time.Now().UTC().Unix(),
		}
		h.trySendDrop(client, outbound{response: Response{Messages: []Message{message}}})
	}
}

func (h *Hub) trySend(client *Client, message outbound) {
	select {
	case <-client.done:
		h.removeClient(client)
		return
	default:
	}
	select {
	case client.send <- message:
	default:
		h.removeClient(client)
	}
}

func (h *Hub) trySendDrop(client *Client, message outbound) {
	select {
	case <-client.done:
		h.removeClient(client)
		return
	default:
	}
	select {
	case client.send <- message:
	default:
	}
}

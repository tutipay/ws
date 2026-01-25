package chat

import (
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	mu sync.RWMutex

	// Registered clients.
	clients map[string]*Client // Mapping client IDs to client object

	// Inbound messages from the clients.
	broadcast chan *Message

	// Status updates for contacts.
	status chan statusUpdate

	// Persistence queue for messages.
	persist chan persistJob

	// Typing state per sender -> recipient.
	typing map[string]map[string]bool

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	// Database reference, we will need to have it down
	db *sqlx.DB

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
	var persist chan persistJob
	if db != nil && cfg.PersistBuffer > 0 {
		persist = make(chan persistJob, cfg.PersistBuffer)
	}
	return &Hub{
		broadcast:  make(chan *Message, cfg.BroadcastBuffer),
		register:   make(chan *Client, cfg.RegisterBuffer),
		unregister: make(chan *Client, cfg.UnregisterBuffer),
		status:     make(chan statusUpdate, cfg.StatusBuffer),
		persist:    persist,
		clients:    make(map[string]*Client),
		typing:     make(map[string]map[string]bool),
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
	h.startOnce.Do(func() {
		for i := 0; i < h.cfg.BroadcastWorkers; i++ {
			go h.broadcastWorker()
		}
		for i := 0; i < h.cfg.StatusWorkers; i++ {
			go h.statusWorker()
		}
		for i := 0; i < h.cfg.PersistWorkers; i++ {
			go h.persistWorker()
		}
	})
	for {
		select {
		case <-h.done:
			return
		case client := <-h.register:
			h.addClient(client)
		case client := <-h.unregister:
			log.Printf("client ID: %s got disconnected", client.ID)
			h.removeClient(client)
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
	from     string
	status   string
	contacts []string
}

type persistJob struct {
	message Message
	done    chan error
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
	if message.Type == "typing" {
		h.handleTyping(message)
		return
	}
	h.clearTypingBetween(message.From, message.To, true)
	if client := h.getClient(message.To); client != nil {
		// We don't need to check for the case of non-existing clients to store the message
		// in the database to send it to them later when they connect, because we store the
		// message in the database in `readPump` before we send it through the broadcast channel
		h.trySend(client, outbound{
			response:    Response{Messages: []Message{*message}},
			markReadIDs: []string{message.ID},
		})
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
			h.trySend(client, outbound{
				response: Response{Status: StatusResponse{Mobile: update.from, ConnectionStatus: update.status}},
			})
		}
	}
}

func (h *Hub) persistWorker() {
	if h.persist == nil {
		return
	}
	batchSize := h.cfg.PersistBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	flushInterval := h.cfg.PersistFlushInterval
	if flushInterval <= 0 {
		flushInterval = 5 * time.Millisecond
	}

	var (
		batch  []persistJob
		timer  *time.Timer
		timerC <-chan time.Time
	)

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}

	flush := func() {
		if len(batch) == 0 {
			return
		}
		messages := make([]Message, 0, len(batch))
		for _, job := range batch {
			messages = append(messages, job.message)
		}
		err := insertBatch(messages, h.db)
		for _, job := range batch {
			select {
			case job.done <- err:
			default:
			}
		}
		batch = batch[:0]
		stopTimer()
	}

	for {
		select {
		case <-h.done:
			flush()
			return
		case job := <-h.persist:
			batch = append(batch, job)
			if len(batch) == 1 {
				if timer == nil {
					timer = time.NewTimer(flushInterval)
				} else {
					timer.Reset(flushInterval)
				}
				timerC = timer.C
			}
			if len(batch) >= batchSize {
				flush()
			}
		case <-timerC:
			flush()
		}
	}
}

func (h *Hub) persistMessage(message Message) error {
	if h.db == nil {
		return nil
	}
	if h.persist == nil {
		return insert(message, h.db)
	}
	job := persistJob{message: message, done: make(chan error, 1)}
	select {
	case h.persist <- job:
	case <-h.done:
		return errors.New("hub stopped")
	}
	select {
	case err := <-job.done:
		return err
	case <-h.done:
		return errors.New("hub stopped")
	}
}

func (h *Hub) addClient(client *Client) {
	var replaced *Client
	h.mu.Lock()
	if existing, ok := h.clients[client.ID]; ok {
		replaced = existing
	}
	h.clients[client.ID] = client
	h.mu.Unlock()

	if replaced != nil {
		replaced.markReplaced()
		h.clearTypingFor(replaced.ID)
		replaced.close()
	}
}

func (h *Hub) removeClient(client *Client) {
	removed := false
	h.mu.Lock()
	if existing, ok := h.clients[client.ID]; ok && existing == client {
		delete(h.clients, client.ID)
		removed = true
	}
	h.mu.Unlock()
	if removed {
		h.clearTypingFor(client.ID)
	}
	client.close()
}

func (h *Hub) getClient(id string) *Client {
	h.mu.RLock()
	client := h.clients[id]
	h.mu.RUnlock()
	return client
}

func (h *Hub) handleTyping(message *Message) {
	if message.To == "" || message.From == "" {
		return
	}
	isTyping := message.IsTyping != nil && *message.IsTyping
	h.setTyping(message.From, message.To, isTyping)
	if client := h.getClient(message.To); client != nil {
		h.trySendDrop(client, outbound{
			response: Response{Messages: []Message{*message}},
		})
	}
}

func (h *Hub) setTyping(from, to string, isTyping bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.typing == nil {
		h.typing = make(map[string]map[string]bool)
	}
	targets, ok := h.typing[from]
	if !ok {
		if !isTyping {
			return
		}
		targets = make(map[string]bool)
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

func (h *Hub) clearTypingFor(from string) {
	h.mu.Lock()
	targets, ok := h.typing[from]
	if !ok {
		h.mu.Unlock()
		return
	}
	contacts := make([]string, 0, len(targets))
	for to := range targets {
		contacts = append(contacts, to)
	}
	delete(h.typing, from)
	h.mu.Unlock()

	for _, to := range contacts {
		if client := h.getClient(to); client != nil {
			isTyping := false
			message := Message{
				From:     from,
				To:       to,
				Type:     "typing",
				IsTyping: &isTyping,
				Date:     time.Now().Unix(),
			}
			h.trySendDrop(client, outbound{response: Response{Messages: []Message{message}}})
		}
	}
}

func (h *Hub) clearTypingBetween(from, to string, notify bool) {
	shouldNotify := false
	h.mu.Lock()
	targets, ok := h.typing[from]
	if ok {
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
			From:     from,
			To:       to,
			Type:     "typing",
			IsTyping: &isTyping,
			Date:     time.Now().Unix(),
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

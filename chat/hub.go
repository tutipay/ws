package chat

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var accept = make(chan chatMessage, 256)
var upgrader = websocket.Upgrader{} // use default options
var upgrader2 = websocket.Upgrader{}
var xbroadcast = make(chan []byte)

type errorHandler struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var data = make(chan []byte, 256)

type chatMessage struct {
	ID          int
	From        int
	To          int
	Message     string
	IsDelivered bool
	ToCard      *string
}

type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func NewHub() *Hub {
	return &Hub{
		broadcast:  xbroadcast,
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		// This registers a new web socket connect client to our hub.
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
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

// ServeWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		conn.Close()
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256), UserID: 0}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func ws(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader2.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		// close connection
		c.Close()
	}

	defer c.Close()
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}

		xbroadcast <- []byte(id)

		select {
		case data := <-accept:
			log.Printf("recv_accept: %d", data.ID)
			err = c.WriteJSON(data)
			if err != nil {
				log.Println("write:", err)

			}
		case <-time.After(10 * time.Second):
			log.Printf("recv_timeout: %s", message)
			verr := errorHandler{Code: "timeout", Message: "No providers found. Try again."}
			err = c.WriteJSON(verr)
			if err != nil {
				log.Println("write:", err)
			}
			c.Close()
			return
		}
	}
}

package chat

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // TODO: This is dangerous, it must be changed
}

// Client is a middleman between the websocket connection and the hub.
// It is important to note that Client should be also
type Client struct {
	ID  string
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan *Message

	// Chat instance to persist the chats
	db *sqlx.DB
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		// The client should send the message as a JSON string that contains the message text as well
		// as the message metadata through the websocket connection.
		_, messageJSON, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}

		messageJSON = bytes.TrimSpace(bytes.Replace(messageJSON, newline, space, -1))

		var message Message
		// we also need to capture the sender's ID here...
		if err := json.Unmarshal(messageJSON, &message); err != nil {
			log.Printf("error: %v", err)
		}
		message.From = c.ID // sender's ID
		message.Date = time.Now().Unix()
		// Note that the `To` and `Text` fields are required and if they are not sent with the message metadata
		// the application will fail.

		// Generating messageID
		message.ID = uuid.New().String() // TODO: check for the unlikely case of collisions.

		// Populate message with corresponding data (inc: text, from and to) fields
		c.hub.broadcast <- &message

		if c.db != nil {
			// We can safely store data into db at this stage
			// db is: c.hub.db
			log.Print("we are in db")
			insert(message, c.db)
		}

	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:

			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Always make a message as a list of messages, to be consistent with the database
			c.conn.WriteJSON(Response{Type: "chat", Message: []Message{*message}})

			// We need to mark this message as read now, because we already sent it to the client
			// otherwise it will be sent again.
			markMessageAsRead(message.ID, c.db)

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// PreviousMessages retrieves all messages that were sent to a senderID but they still didn't
// Read it.
// We will need to have a reference to a message instance (You can get one via: NewMessage()) and that will be populated
// with a *sqlx.DB instance
func (c *Client) PreviousMessages() {
	chats, err := getUnreadMessages(c.ID, c.db)
	if err != nil {
		log.Printf("error: %v", err)
		return
	}

	// This `if` guard is here because we don't want to send `null` when there are no unread messages
	if len(chats) > 0 {
		c.conn.WriteJSON(Response{Type: "chat", Message: chats})
		updateStatus(c.ID, c.db)
	}
}

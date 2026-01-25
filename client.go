package chat

import (
	"bytes"
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

// Client is a middleman between the websocket connection and the hub.
// It is important to note that Client should be also
type Client struct {
	ID  string
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan outbound

	// Chat instance to persist the chats
	db *sqlx.DB

	done      chan struct{}
	closeOnce sync.Once
	replaced  uint32
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregisterClient(c)
		if !c.isReplaced() {
			c.NotifyStatus("offline")
		}
		c.close()
	}()
	c.conn.SetReadLimit(c.hub.cfg.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongWait))
		return nil
	})
	for {
		// The client should send the message as a JSON string that contains the message text as well
		// as the message metadata through the websocket connection.
		_, messageJSON, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("readPump error: %v", err)
			}
			break
		}

		messageJSON = bytes.TrimSpace(bytes.Replace(messageJSON, newline, space, -1))

		var message Message
		// we also need to capture the sender's ID here...
		if err := json.Unmarshal(messageJSON, &message); err != nil {
			log.Printf("readPump Unmarshal JSON error: %v", err)
			continue
		}
		if message.Type == "typing" {
			if message.To == "" {
				log.Printf("readPump typing validation error: missing to")
				continue
			}
			message.From = c.ID
			message.Date = time.Now().Unix()
			if ok := c.hub.broadcastMessage(&message); !ok {
				return
			}
			continue
		}

		if message.To == "" || message.Text == "" {
			log.Printf("readPump validation error: missing to or text")
			continue
		}
		message.From = c.ID // sender's ID
		message.Date = time.Now().Unix()
		// Note that the `To` and `Text` fields are required and if they are not sent with the message metadata
		// the application will fail.

		// Generating messageID
		message.ID = uuid.New().String() // TODO: check for the unlikely case of collisions.

		// Populate message with corresponding data (inc: text, from and to) fields
		if ok := c.hub.broadcastMessage(&message); !ok {
			return
		}

		if c.db != nil {
			// We can safely store data into db at this stage
			// db is: c.hub.db
			if err := insert(message, c.db); err != nil {
				log.Printf("readPump insert error: %v", err)
			}
		}

	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(c.hub.cfg.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteWait))

			if err := c.conn.WriteJSON(message.response); err != nil {
				return
			}

			// We need to mark this message as read now, because we already sent it to the client
			// otherwise it will be sent again.
			if len(message.markReadIDs) > 0 && c.db != nil {
				if err := markMessagesAsRead(message.markReadIDs, c.db); err != nil {
					log.Printf("writePump markMessagesAsRead error: %v", err)
				}
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
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
		log.Printf("getUnreadMessages error: %v", err)
		return
	}

	// This `if` guard is here because we don't want to send `null` when there are no unread messages
	if len(chats) > 0 {
		messageIDs := make([]string, 0, len(chats))
		for _, chat := range chats {
			messageIDs = append(messageIDs, chat.ID)
		}
		_ = c.enqueue(outbound{
			response:    Response{Messages: chats},
			markReadIDs: messageIDs,
		})
	}
}

// ShareStatus will send `status` messages to all connected clients that have registered
// the current client as a contact.
func (c *Client) ShareStatus(status string) {
	contacts, err := getContacts(c.ID, c.db)
	if err == nil {
		_ = c.hub.queueStatus(statusUpdate{
			from:     c.ID,
			status:   status,
			contacts: contacts,
		})
	}
}

func (c *Client) NotifyStatus(status string) {
	c.ShareStatus(status)
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (c *Client) enqueue(message outbound) bool {
	select {
	case c.send <- message:
		return true
	case <-c.done:
		return false
	}
}

func (c *Client) markReplaced() {
	atomic.StoreUint32(&c.replaced, 1)
}

func (c *Client) isReplaced() bool {
	return atomic.LoadUint32(&c.replaced) == 1
}

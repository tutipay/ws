package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
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

// Client is one authenticated tenant/user websocket connection.
type Client struct {
	Identity ClientIdentity
	hub      *Hub
	conn     *websocket.Conn
	send     chan outbound
	db       *sqlx.DB

	done      chan struct{}
	closeOnce sync.Once
	replaced  uint32

	sessionContext          context.Context
	cancelSessionValidation context.CancelFunc
}

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
		return c.conn.SetReadDeadline(time.Now().Add(c.hub.cfg.PongWait))
	})
	for {
		messageType, messageJSON, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("readPump websocket error: %v", err)
			}
			return
		}
		if messageType != websocket.TextMessage {
			if !c.sendProtocolError("bad_frame", "") {
				return
			}
			continue
		}
		messageJSON = bytes.TrimSpace(bytes.ReplaceAll(messageJSON, newline, space))
		frame, err := decodeClientFrame(messageJSON)
		if err != nil {
			if !c.sendProtocolError("bad_frame", "") {
				return
			}
			continue
		}
		switch frame.Type {
		case FrameTypeMessage:
			if !c.handleMessageFrame(frame) {
				return
			}
		case FrameTypeTyping:
			if !c.handleTypingFrame(frame) {
				return
			}
		case FrameTypeAck:
			if !c.handleAckFrame(frame) {
				return
			}
		default:
			if !c.sendProtocolError("bad_frame", frame.ID) {
				return
			}
		}
	}
}

func decodeClientFrame(data []byte) (clientFrame, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var frame clientFrame
	if err := decoder.Decode(&frame); err != nil {
		return clientFrame{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return clientFrame{}, err
	}
	return frame, nil
}

func (c *Client) handleMessageFrame(frame clientFrame) bool {
	if !canonicalMessageID(frame.ID) || frame.ToUserID <= 0 ||
		strings.TrimSpace(frame.Text) == "" || frame.IsTyping != nil || len(frame.MessageIDs) != 0 {
		return c.sendProtocolError("bad_frame", frame.ID)
	}
	message := Message{
		TenantID:   c.Identity.TenantID,
		ID:         frame.ID,
		FromUserID: c.Identity.UserID,
		ToUserID:   frame.ToUserID,
		Text:       frame.Text,
		Date:       time.Now().UTC().Unix(),
		Type:       FrameTypeMessage,
	}
	result, err := c.hub.persistMessage(message)
	if err != nil {
		if errors.Is(err, ErrMessageConflict) {
			return c.sendProtocolError("message_conflict", frame.ID)
		}
		return c.sendProtocolError("persistence_unavailable", frame.ID)
	}
	if !c.enqueue(outbound{response: Response{Ack: &Acknowledgement{
		Kind:       AckKindPersisted,
		MessageIDs: []string{frame.ID},
	}}}) {
		return false
	}
	result.Message.Type = FrameTypeMessage
	if result.Created || !result.Message.IsDelivered {
		return c.hub.broadcastMessage(&result.Message)
	}
	return true
}

func (c *Client) handleTypingFrame(frame clientFrame) bool {
	if frame.ID != "" || frame.ToUserID <= 0 || frame.Text != "" ||
		frame.IsTyping == nil || len(frame.MessageIDs) != 0 {
		return c.sendProtocolError("bad_frame", frame.ID)
	}
	message := Message{
		TenantID:   c.Identity.TenantID,
		FromUserID: c.Identity.UserID,
		ToUserID:   frame.ToUserID,
		Date:       time.Now().UTC().Unix(),
		Type:       FrameTypeTyping,
		IsTyping:   frame.IsTyping,
	}
	return c.hub.broadcastMessage(&message)
}

func (c *Client) handleAckFrame(frame clientFrame) bool {
	if frame.ID != "" || frame.ToUserID != 0 || frame.Text != "" || frame.IsTyping != nil {
		return c.sendProtocolError("bad_frame", "")
	}
	messageIDs, ok := canonicalMessageIDs(frame.MessageIDs)
	if !ok {
		return c.sendProtocolError("bad_frame", "")
	}
	delivered, err := markMessagesDelivered(c.Identity, messageIDs, c.db, c.hub.cfg.MarkReadBatch)
	if err != nil {
		return c.sendProtocolError("persistence_unavailable", "")
	}
	return c.enqueue(outbound{response: Response{Ack: &Acknowledgement{
		Kind:       AckKindDelivered,
		MessageIDs: delivered,
	}}})
}

func canonicalMessageID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func canonicalMessageIDs(values []string) ([]string, bool) {
	if len(values) == 0 || len(values) > 200 {
		return nil, false
	}
	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !canonicalMessageID(value) {
			return nil, false
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique, true
}

func (c *Client) sendProtocolError(code, messageID string) bool {
	return c.enqueue(outbound{response: Response{Error: &ErrorResponse{Code: code, MessageID: messageID}}})
}

func (c *Client) writePump() {
	pingTicker := time.NewTicker(c.hub.cfg.PingPeriod)
	var validationTicker *time.Ticker
	var validation <-chan time.Time
	if c.hub.cfg.ValidateClientSession != nil {
		validationTicker = time.NewTicker(c.hub.cfg.SessionValidationInterval)
		validation = validationTicker.C
	}
	defer func() {
		pingTicker.Stop()
		if validationTicker != nil {
			validationTicker.Stop()
		}
		c.close()
	}()
	for {
		select {
		case message := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteJSON(message.response); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(c.hub.cfg.WriteWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-validation:
			if err := c.hub.cfg.ValidateClientSession(c.sessionContext); err != nil {
				_, closeCode, message := sessionValidationFailure(err)
				_ = c.conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(closeCode, message),
					time.Now().Add(c.hub.cfg.WriteWait),
				)
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) sendPreviousMessages(messages []Message) {
	if len(messages) == 0 {
		return
	}
	batchSize := c.hub.cfg.UnreadBatchSize
	if batchSize <= 0 || batchSize > len(messages) {
		batchSize = len(messages)
	}
	for start := 0; start < len(messages); start += batchSize {
		end := start + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		if !c.enqueue(outbound{response: Response{Messages: messages[start:end]}}) {
			return
		}
	}
}

func (c *Client) ShareStatus(status string) {
	contacts, err := getContactOwners(c.Identity, c.db)
	if err != nil {
		return
	}
	_ = c.hub.queueStatus(statusUpdate{from: c.Identity, status: status, contacts: contacts})
}

func (c *Client) NotifyStatus(status string) {
	c.ShareStatus(status)
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		if c.cancelSessionValidation != nil {
			c.cancelSessionValidation()
		}
		close(c.done)
		_ = c.conn.Close()
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

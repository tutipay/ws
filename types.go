package chat

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	FrameTypeMessage = "message"
	FrameTypeTyping  = "typing"
	FrameTypeAck     = "ack"

	AckKindPersisted = "persisted"
	AckKindDelivered = "delivered"
)

var ErrInvalidClientIdentity = errors.New("invalid client identity")

// ClientIdentity is the authenticated, tenant-scoped identity of one socket.
// It must be derived by the server; neither field is accepted from websocket
// frames.
type ClientIdentity struct {
	TenantID string
	UserID   string
}

func (identity ClientIdentity) validate() error {
	if !canonicalIdentifier(identity.TenantID) || !canonicalIdentifier(identity.UserID) {
		return ErrInvalidClientIdentity
	}
	return nil
}

func canonicalIdentifier(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// Message is a server-authored chat or typing event. TenantID is deliberately
// excluded from the wire representation: the socket's authenticated tenant is
// the only routing scope.
type Message struct {
	TenantID   string `db:"tenant_id" json:"-"`
	ID         string `db:"id" json:"id,omitempty"`
	FromUserID string `db:"from_user_id" json:"from_user_id"`
	ToUserID   string `db:"to_user_id" json:"to_user_id"`
	Text       string `db:"text" json:"text,omitempty"`
	Date       int64  `db:"date" json:"date"`
	Type       string `db:"-" json:"type"`
	IsTyping   *bool  `db:"-" json:"is_typing,omitempty"`

	IsDelivered bool `db:"is_delivered" json:"-"`
}

func (message Message) sender() ClientIdentity {
	return ClientIdentity{TenantID: message.TenantID, UserID: message.FromUserID}
}

func (message Message) recipient() ClientIdentity {
	return ClientIdentity{TenantID: message.TenantID, UserID: message.ToUserID}
}

// StatusResponse reports presence for a stable user ID in the receiver's
// authenticated tenant.
type StatusResponse struct {
	UserID           string `json:"user_id"`
	ConnectionStatus string `json:"connection_status"`
}

type Acknowledgement struct {
	Kind       string   `json:"kind"`
	MessageIDs []string `json:"message_ids"`
}

type ErrorResponse struct {
	Code      string `json:"code"`
	MessageID string `json:"message_id,omitempty"`
}

// Response is the only server-to-client websocket envelope.
type Response struct {
	Status   *StatusResponse  `json:"status,omitempty"`
	Messages []Message        `json:"messages,omitempty"`
	Ack      *Acknowledgement `json:"ack,omitempty"`
	Error    *ErrorResponse   `json:"error,omitempty"`
}

type clientFrame struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	ToUserID   string   `json:"to_user_id"`
	Text       string   `json:"text"`
	IsTyping   *bool    `json:"is_typing"`
	MessageIDs []string `json:"message_ids"`
}

type outbound struct {
	response Response
}

func marshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}

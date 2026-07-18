package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

var (
	ErrUnauthorized                 = errors.New("unauthorized")
	ErrBadRequest                   = errors.New("bad request")
	ErrSessionValidationUnavailable = errors.New("session validation unavailable")
)

func sessionValidationFailure(err error) (int, int, string) {
	if errors.Is(err, ErrSessionValidationUnavailable) {
		return http.StatusServiceUnavailable, websocket.CloseInternalServerErr, ErrSessionValidationUnavailable.Error()
	}
	return http.StatusUnauthorized, websocket.ClosePolicyViolation, ErrUnauthorized.Error()
}

// ServeWs upgrades one authenticated, tenant-scoped client. The embedding
// application must derive ClientIdentity from trusted request state.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	if hub == nil || hub.cfg.ClientIdentityFromRequest == nil {
		http.Error(w, "client identity resolver is required", http.StatusInternalServerError)
		return
	}
	identity, err := hub.cfg.ClientIdentityFromRequest(r)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, ErrBadRequest) {
			status = http.StatusBadRequest
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	if err := identity.validate(); err != nil {
		http.Error(w, ErrUnauthorized.Error(), http.StatusUnauthorized)
		return
	}
	if hub.cfg.ValidateClientSession != nil {
		if err := hub.cfg.ValidateClientSession(r.Context()); err != nil {
			status, _, message := sessionValidationFailure(err)
			http.Error(w, message, status)
			return
		}
	}
	if hub.db == nil || hub.db.PingContext(r.Context()) != nil {
		http.Error(w, ErrPersistenceUnavailable.Error(), http.StatusServiceUnavailable)
		return
	}

	conn, err := hub.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ServeWs upgrader error: %v", err)
		return
	}

	var sessionContext context.Context
	var cancelSessionValidation context.CancelFunc
	if hub.cfg.ValidateClientSession != nil {
		sessionContext, cancelSessionValidation = context.WithCancel(context.WithoutCancel(r.Context()))
	}
	client := &Client{
		db:                      hub.db,
		Identity:                identity,
		hub:                     hub,
		conn:                    conn,
		send:                    make(chan outbound, hub.cfg.ClientSendBuffer),
		done:                    make(chan struct{}),
		sessionContext:          sessionContext,
		cancelSessionValidation: cancelSessionValidation,
	}
	if ok := hub.registerClient(client); !ok {
		client.close()
		return
	}
	backlog, err := getUnreadMessages(identity, hub.cfg.MaxUnreadMessages, hub.db)
	if err != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, ErrPersistenceUnavailable.Error()),
			time.Now().Add(hub.cfg.WriteWait),
		)
		hub.unregisterClient(client)
		client.close()
		return
	}

	go client.writePump()
	go client.readPump()
	client.sendPreviousMessages(backlog)
	client.NotifyStatus("online")
}

type ContactsRequest struct {
	UserID int64 `json:"user_id"`
}

// SubmitContacts is a small HTTP adapter for applications that already
// resolved contacts to stable user IDs. Applications receiving phone numbers
// must resolve them in their identity service before calling this function.
func SubmitContacts(currentUser ClientIdentity, db *sqlx.DB, w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var contacts []ContactsRequest
	if err := decoder.Decode(&contacts); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	userIDs := make([]int64, 0, len(contacts))
	for _, contact := range contacts {
		userIDs = append(userIDs, contact.UserID)
	}
	if err := AddContacts(r.Context(), currentUser, userIDs, db); err != nil {
		if errors.Is(err, ErrInvalidContactBatch) || errors.Is(err, ErrInvalidClientIdentity) {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrPersistenceUnavailable) {
			http.Error(w, ErrPersistenceUnavailable.Error(), http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "could not save contacts", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(marshal(contacts))
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrBadRequest
		}
		return err
	}
	return nil
}

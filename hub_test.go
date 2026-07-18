package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

const (
	testTenantHeader = "X-Test-Tenant"
	testUserHeader   = "X-Test-User"
)

type sessionContextKey struct{}

func testIdentityResolver(r *http.Request) (ClientIdentity, error) {
	return ClientIdentity{TenantID: r.Header.Get(testTenantHeader), UserID: r.Header.Get(testUserHeader)}, nil
}

func newSocketServer(t *testing.T, db *sqlx.DB, configure func(*HubConfig)) (*Hub, string) {
	t.Helper()
	cfg := DefaultHubConfig()
	cfg.PingPeriod = time.Hour
	cfg.ClientIdentityFromRequest = testIdentityResolver
	if configure != nil {
		configure(&cfg)
	}
	hub := NewHubWithConfig(db, cfg)
	go hub.Run()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	t.Cleanup(func() {
		server.Close()
		hub.Stop()
	})
	return hub, "ws" + strings.TrimPrefix(server.URL, "http")
}

func dialIdentity(t *testing.T, wsURL string, identity ClientIdentity) *websocket.Conn {
	t.Helper()
	headers := http.Header{}
	headers.Set(testTenantHeader, identity.TenantID)
	headers.Set(testUserHeader, identity.UserID)
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("dial %#v: response=%v error=%v", identity, response, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendMessage(t *testing.T, conn *websocket.Conn, id, to, text string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{
		"type": FrameTypeMessage, "id": id, "to_user_id": to, "text": text,
	}); err != nil {
		t.Fatalf("send message: %v", err)
	}
}

func sendAck(t *testing.T, conn *websocket.Conn, ids ...string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]any{"type": FrameTypeAck, "message_ids": ids}); err != nil {
		t.Fatalf("send ack: %v", err)
	}
}

func readResponse(t *testing.T, conn *websocket.Conn) Response {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response Response
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("read response: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return response
}

func requireAck(t *testing.T, response Response, kind, id string) {
	t.Helper()
	if response.Ack == nil || response.Ack.Kind != kind || len(response.Ack.MessageIDs) != 1 || response.Ack.MessageIDs[0] != id {
		t.Fatalf("ack = %#v, want kind=%s id=%s", response, kind, id)
	}
}

func requireMessage(t *testing.T, response Response, id, from, to, text string) Message {
	t.Helper()
	if len(response.Messages) != 1 {
		t.Fatalf("messages = %#v, want one", response)
	}
	message := response.Messages[0]
	if message.ID != id || message.FromUserID != from || message.ToUserID != to || message.Text != text ||
		message.Type != FrameTypeMessage || message.Date <= 0 || message.TenantID != "" || message.IsDelivered {
		t.Fatalf("message = %#v", message)
	}
	return message
}

func assertNoFrame(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unexpected websocket frame")
	}
}

func TestServeWs_MessageRequiresRecipientAck(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	sender := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "sender"))
	recipient := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "recipient"))
	id := uuid.NewString()

	sendMessage(t, sender, id, "recipient", "hello")
	requireAck(t, readResponse(t, sender), AckKindPersisted, id)
	requireMessage(t, readResponse(t, recipient), id, "sender", "recipient", "hello")

	unread, err := getUnreadMessages(testIdentity("tenant-alpha", "recipient"), 0, db)
	if err != nil || len(unread) != 1 {
		t.Fatalf("unread before recipient ack = %#v, %v", unread, err)
	}
	sendAck(t, recipient, id)
	requireAck(t, readResponse(t, recipient), AckKindDelivered, id)
	unread, err = getUnreadMessages(testIdentity("tenant-alpha", "recipient"), 0, db)
	if err != nil || len(unread) != 0 {
		t.Fatalf("unread after recipient ack = %#v, %v", unread, err)
	}
}

func TestServeWs_SameUserIDsAndMessageIDCoexistAcrossTenants(t *testing.T) {
	db := newTestDB(t)
	hub, wsURL := newSocketServer(t, db, nil)
	alphaSender := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-sender"))
	alphaRecipient := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-recipient"))
	betaSender := dialIdentity(t, wsURL, testIdentity("tenant-beta", "same-sender"))
	betaRecipient := dialIdentity(t, wsURL, testIdentity("tenant-beta", "same-recipient"))
	waitForClientCount(t, hub, 4)
	id := uuid.NewString()

	sendMessage(t, alphaSender, id, "same-recipient", "alpha-only")
	sendMessage(t, betaSender, id, "same-recipient", "beta-only")
	requireAck(t, readResponse(t, alphaSender), AckKindPersisted, id)
	requireAck(t, readResponse(t, betaSender), AckKindPersisted, id)
	requireMessage(t, readResponse(t, alphaRecipient), id, "same-sender", "same-recipient", "alpha-only")
	requireMessage(t, readResponse(t, betaRecipient), id, "same-sender", "same-recipient", "beta-only")

	sendAck(t, alphaRecipient, id)
	requireAck(t, readResponse(t, alphaRecipient), AckKindDelivered, id)
	alphaUnread, _ := getUnreadMessages(testIdentity("tenant-alpha", "same-recipient"), 0, db)
	betaUnread, _ := getUnreadMessages(testIdentity("tenant-beta", "same-recipient"), 0, db)
	if len(alphaUnread) != 0 || len(betaUnread) != 1 || betaUnread[0].Text != "beta-only" {
		t.Fatalf("cross-tenant ack: alpha=%#v beta=%#v", alphaUnread, betaUnread)
	}
}

func TestServeWs_ReplacementIsTenantScoped(t *testing.T) {
	db := newTestDB(t)
	hub, wsURL := newSocketServer(t, db, nil)
	alphaOld := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-user"))
	beta := dialIdentity(t, wsURL, testIdentity("tenant-beta", "same-user"))
	betaRecipient := dialIdentity(t, wsURL, testIdentity("tenant-beta", "recipient"))
	waitForClientCount(t, hub, 3)
	_ = dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-user"))
	waitForClientCount(t, hub, 3)

	_ = alphaOld.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := alphaOld.ReadMessage(); err == nil {
		t.Fatal("replaced alpha connection remained open")
	}
	typing := true
	if err := beta.WriteJSON(map[string]any{
		"type": FrameTypeTyping, "to_user_id": "recipient", "is_typing": typing,
	}); err != nil {
		t.Fatalf("beta typing after alpha replacement: %v", err)
	}
	response := readResponse(t, betaRecipient)
	if len(response.Messages) != 1 || response.Messages[0].FromUserID != "same-user" || response.Messages[0].IsTyping == nil || !*response.Messages[0].IsTyping {
		t.Fatalf("beta typing response = %#v", response)
	}
}

func TestServeWs_RejectsForgedServerFields(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	sender := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "sender"))
	id := uuid.NewString()
	if err := sender.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
		`{"type":"message","id":%q,"to_user_id":"recipient","text":"forged","tenant_id":"tenant-beta","from_user_id":"attacker","date":1,"is_delivered":true}`,
		id,
	))); err != nil {
		t.Fatalf("write forged frame: %v", err)
	}
	response := readResponse(t, sender)
	if response.Error == nil || response.Error.Code != "bad_frame" {
		t.Fatalf("forged response = %#v", response)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM chats_v2`); err != nil || count != 0 {
		t.Fatalf("forged frame persisted: count=%d err=%v", count, err)
	}

	// A rejected frame does not poison the authenticated socket.
	recipient := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "recipient"))
	sendMessage(t, sender, id, "recipient", "server-authored")
	requireAck(t, readResponse(t, sender), AckKindPersisted, id)
	requireMessage(t, readResponse(t, recipient), id, "sender", "recipient", "server-authored")
}

func TestServeWs_ExactRetryAndChangedPayloadConflict(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	sender := dialIdentity(t, wsURL, testIdentity("tenant", "sender"))
	recipient := dialIdentity(t, wsURL, testIdentity("tenant", "recipient"))
	id := uuid.NewString()

	sendMessage(t, sender, id, "recipient", "once")
	requireAck(t, readResponse(t, sender), AckKindPersisted, id)
	requireMessage(t, readResponse(t, recipient), id, "sender", "recipient", "once")
	sendAck(t, recipient, id)
	requireAck(t, readResponse(t, recipient), AckKindDelivered, id)

	sendMessage(t, sender, id, "recipient", "once")
	requireAck(t, readResponse(t, sender), AckKindPersisted, id)
	sendMessage(t, sender, id, "recipient", "changed")
	response := readResponse(t, sender)
	if response.Error == nil || response.Error.Code != "message_conflict" || response.Error.MessageID != id {
		t.Fatalf("changed retry response = %#v", response)
	}
	var count int
	var text string
	if err := db.QueryRowx(`SELECT count(*), min(text) FROM chats_v2 WHERE tenant_id = ? AND id = ?`, "tenant", id).Scan(&count, &text); err != nil {
		t.Fatalf("query persisted message: %v", err)
	}
	if count != 1 || text != "once" {
		t.Fatalf("persisted row count=%d text=%q", count, text)
	}
	assertNoFrame(t, recipient)
}

func TestServeWs_OfflineBacklogRemainsUntilAck(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	sender := dialIdentity(t, wsURL, testIdentity("tenant", "sender"))
	id := uuid.NewString()
	sendMessage(t, sender, id, "recipient", "offline")
	requireAck(t, readResponse(t, sender), AckKindPersisted, id)

	first := dialIdentity(t, wsURL, testIdentity("tenant", "recipient"))
	requireMessage(t, readResponse(t, first), id, "sender", "recipient", "offline")
	_ = first.Close()

	second := dialIdentity(t, wsURL, testIdentity("tenant", "recipient"))
	requireMessage(t, readResponse(t, second), id, "sender", "recipient", "offline")
	sendAck(t, second, id)
	requireAck(t, readResponse(t, second), AckKindDelivered, id)
	_ = second.Close()

	third := dialIdentity(t, wsURL, testIdentity("tenant", "recipient"))
	assertNoFrame(t, third)
}

func TestServeWs_TypingCannotCrossTenant(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	sender := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "sender"))
	alphaRecipient := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-recipient"))
	betaRecipient := dialIdentity(t, wsURL, testIdentity("tenant-beta", "same-recipient"))
	if err := sender.WriteJSON(map[string]any{
		"type": FrameTypeTyping, "to_user_id": "same-recipient", "is_typing": true,
	}); err != nil {
		t.Fatalf("send typing: %v", err)
	}
	response := readResponse(t, alphaRecipient)
	if len(response.Messages) != 1 || response.Messages[0].FromUserID != "sender" || response.Messages[0].IsTyping == nil || !*response.Messages[0].IsTyping {
		t.Fatalf("alpha typing = %#v", response)
	}
	assertNoFrame(t, betaRecipient)
}

func TestServeWs_PresenceCannotCrossTenant(t *testing.T) {
	db := newTestDB(t)
	if err := AddContacts(context.Background(), testIdentity("tenant-alpha", "observer"), []string{"same-subject"}, db); err != nil {
		t.Fatalf("add alpha contact: %v", err)
	}
	if err := AddContacts(context.Background(), testIdentity("tenant-beta", "observer"), []string{"same-subject"}, db); err != nil {
		t.Fatalf("add beta contact: %v", err)
	}
	_, wsURL := newSocketServer(t, db, nil)
	alphaObserver := dialIdentity(t, wsURL, testIdentity("tenant-alpha", "observer"))
	betaObserver := dialIdentity(t, wsURL, testIdentity("tenant-beta", "observer"))
	_ = dialIdentity(t, wsURL, testIdentity("tenant-alpha", "same-subject"))
	response := readResponse(t, alphaObserver)
	if response.Status == nil || response.Status.UserID != "same-subject" || response.Status.ConnectionStatus != "online" {
		t.Fatalf("alpha presence = %#v", response)
	}
	assertNoFrame(t, betaObserver)
}

func TestServeWs_RequiresTrustedIdentityResolver(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run()
	defer hub.Stop()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer server.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("missing resolver response=%v error=%v", response, err)
	}
}

func TestServeWs_RejectsInvalidInitialSession(t *testing.T) {
	for _, tt := range []struct {
		name       string
		validation error
		wantStatus int
	}{
		{name: "revoked", validation: errors.New("revoked"), wantStatus: http.StatusUnauthorized},
		{name: "unavailable", validation: ErrSessionValidationUnavailable, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, wsURL := newSocketServer(t, nil, func(cfg *HubConfig) {
				cfg.ValidateClientSession = func(context.Context) error { return tt.validation }
			})
			headers := http.Header{testTenantHeader: []string{"tenant"}, testUserHeader: []string{"user"}}
			conn, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
			if conn != nil {
				_ = conn.Close()
			}
			if err == nil || response == nil || response.StatusCode != tt.wantStatus {
				t.Fatalf("expected HTTP %d, response=%v error=%v", tt.wantStatus, response, err)
			}
		})
	}
}

func TestServeWs_ClosesRevokedSession(t *testing.T) {
	for _, tt := range []struct {
		name       string
		validation error
		wantClose  int
	}{
		{name: "revoked", validation: errors.New("revoked"), wantClose: websocket.ClosePolicyViolation},
		{name: "unavailable", validation: ErrSessionValidationUnavailable, wantClose: websocket.CloseInternalServerErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var invalid atomic.Bool
			var validations atomic.Int32
			cfg := DefaultHubConfig()
			cfg.PingPeriod = time.Hour
			cfg.SessionValidationInterval = 10 * time.Millisecond
			cfg.ClientIdentityFromRequest = testIdentityResolver
			cfg.ValidateClientSession = func(ctx context.Context) error {
				validations.Add(1)
				if ctx.Value(sessionContextKey{}) != "session-a" {
					return errors.New("missing session context")
				}
				if invalid.Load() {
					return tt.validation
				}
				return nil
			}
			hub := NewHubWithConfig(nil, cfg)
			go hub.Run()
			defer hub.Stop()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := context.WithValue(r.Context(), sessionContextKey{}, "session-a")
				ServeWs(hub, w, r.WithContext(ctx))
			}))
			defer server.Close()
			headers := http.Header{testTenantHeader: []string{"tenant"}, testUserHeader: []string{"user"}}
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), headers)
			if err != nil {
				t.Fatalf("dial websocket: %v", err)
			}
			defer conn.Close()
			invalid.Store(true)
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			_, _, err = conn.ReadMessage()
			if !websocket.IsCloseError(err, tt.wantClose) {
				t.Fatalf("expected close code %d, got %v", tt.wantClose, err)
			}
			if validations.Load() < 2 {
				t.Fatalf("validation calls = %d, want at least 2", validations.Load())
			}
		})
	}
}

func TestServeWs_CancelsValidationWhenClientDisconnects(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var validations atomic.Int32
	_, wsURL := newSocketServer(t, nil, func(cfg *HubConfig) {
		cfg.SessionValidationInterval = 10 * time.Millisecond
		cfg.ValidateClientSession = func(ctx context.Context) error {
			if validations.Add(1) == 1 {
				return nil
			}
			close(started)
			<-ctx.Done()
			close(cancelled)
			return ctx.Err()
		}
	})
	conn := dialIdentity(t, wsURL, testIdentity("tenant", "user"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("periodic validation did not start")
	}
	_ = conn.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("validation context was not cancelled")
	}
}

func TestHub_ConcurrentTenantRouting(t *testing.T) {
	db := newTestDB(t)
	_, wsURL := newSocketServer(t, db, nil)
	const tenants = 8
	type pair struct {
		sender    *websocket.Conn
		recipient *websocket.Conn
		tenant    string
		id        string
	}
	pairs := make([]pair, 0, tenants)
	for index := range tenants {
		tenant := fmt.Sprintf("tenant-%d", index)
		pairs = append(pairs, pair{
			sender:    dialIdentity(t, wsURL, testIdentity(tenant, "same-sender")),
			recipient: dialIdentity(t, wsURL, testIdentity(tenant, "same-recipient")),
			tenant:    tenant, id: uuid.NewString(),
		})
	}
	var writes sync.WaitGroup
	for index := range pairs {
		writes.Add(1)
		go func(item pair) {
			defer writes.Done()
			_ = item.sender.WriteJSON(map[string]any{
				"type": FrameTypeMessage, "id": item.id, "to_user_id": "same-recipient", "text": item.tenant,
			})
		}(pairs[index])
	}
	writes.Wait()
	for _, item := range pairs {
		requireAck(t, readResponse(t, item.sender), AckKindPersisted, item.id)
		requireMessage(t, readResponse(t, item.recipient), item.id, "same-sender", "same-recipient", item.tenant)
	}
}

func waitForClientCount(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		count := len(hub.clients)
		hub.mu.RUnlock()
		if count == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client count did not reach %d", want)
}

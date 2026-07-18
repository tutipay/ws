package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type sessionContextKey struct{}

func TestServeWs_DirectMessage(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connA, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=client-a", nil)
	if err != nil {
		t.Fatalf("dial client-a: %v", err)
	}
	defer connA.Close()

	connB, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=client-b", nil)
	if err != nil {
		t.Fatalf("dial client-b: %v", err)
	}
	defer connB.Close()

	if err := connA.WriteJSON(Message{To: "client-b", Text: "hello"}); err != nil {
		t.Fatalf("write json: %v", err)
	}

	_ = connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response Response
	if err := connB.ReadJSON(&response); err != nil {
		t.Fatalf("read json: %v", err)
	}
	if len(response.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(response.Messages))
	}
	if response.Messages[0].From != "client-a" {
		t.Fatalf("expected from client-a, got %q", response.Messages[0].From)
	}
	if response.Messages[0].Text != "hello" {
		t.Fatalf("expected text hello, got %q", response.Messages[0].Text)
	}
}

func TestServeWs_TypingEvent(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connA, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=typer", nil)
	if err != nil {
		t.Fatalf("dial typer: %v", err)
	}
	defer connA.Close()

	connB, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=receiver", nil)
	if err != nil {
		t.Fatalf("dial receiver: %v", err)
	}
	defer connB.Close()

	if err := connA.WriteMessage(websocket.TextMessage, []byte(`{"type":"typing","to":"receiver","is_typing":true}`)); err != nil {
		t.Fatalf("write typing: %v", err)
	}

	_ = connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response Response
	if err := connB.ReadJSON(&response); err != nil {
		t.Fatalf("read json: %v", err)
	}
	if len(response.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(response.Messages))
	}
	if response.Messages[0].Type != "typing" {
		t.Fatalf("expected typing message, got %q", response.Messages[0].Type)
	}
	if response.Messages[0].IsTyping == nil || !*response.Messages[0].IsTyping {
		t.Fatalf("expected is_typing true")
	}
}

func TestServeWs_TypingEventWithoutType(t *testing.T) {
	hub := NewHub(nil)
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connA, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=typer", nil)
	if err != nil {
		t.Fatalf("dial typer: %v", err)
	}
	defer connA.Close()

	connB, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=receiver", nil)
	if err != nil {
		t.Fatalf("dial receiver: %v", err)
	}
	defer connB.Close()

	if err := connA.WriteMessage(websocket.TextMessage, []byte(`{"to":"receiver","is_typing":true}`)); err != nil {
		t.Fatalf("write typing: %v", err)
	}

	_ = connB.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response Response
	if err := connB.ReadJSON(&response); err != nil {
		t.Fatalf("read json: %v", err)
	}
	if len(response.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(response.Messages))
	}
	if response.Messages[0].Type != "typing" {
		t.Fatalf("expected typing message, got %q", response.Messages[0].Type)
	}
	if response.Messages[0].IsTyping == nil || !*response.Messages[0].IsTyping {
		t.Fatalf("expected is_typing true")
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
			cfg := DefaultHubConfig()
			cfg.ValidateClientSession = func(context.Context) error { return tt.validation }
			hub := NewHubWithConfig(nil, cfg)
			go hub.Run()
			defer hub.Stop()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ServeWs(hub, w, r)
			}))
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, response, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=client-a", nil)
			if conn != nil {
				conn.Close()
			}
			if err == nil {
				t.Fatal("expected websocket upgrade to fail")
			}
			if response == nil || response.StatusCode != tt.wantStatus {
				t.Fatalf("expected HTTP %d, got %#v", tt.wantStatus, response)
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

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=client-a", nil)
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
				t.Fatalf("expected initial and periodic validation, got %d calls", validations.Load())
			}
		})
	}
}

func TestServeWs_CancelsValidationWhenClientDisconnects(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var validations atomic.Int32
	cfg := DefaultHubConfig()
	cfg.PingPeriod = time.Hour
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
	hub := NewHubWithConfig(nil, cfg)
	go hub.Run()
	defer hub.Stop()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeWs(hub, w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/?clientID=client-a", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
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

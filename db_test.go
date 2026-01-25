package chat

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := OpenDb(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func newMessage(to string) Message {
	return Message{
		ID:   uuid.New().String(),
		From: "sender",
		To:   to,
		Text: "hello",
		Date: time.Now().Unix(),
	}
}

func TestChat_insertAndGetUnreadMessages(t *testing.T) {
	db := newTestDB(t)
	msg1 := newMessage("user-1")
	msg2 := newMessage("user-1")

	if err := insert(msg1, db); err != nil {
		t.Fatalf("insert msg1: %v", err)
	}
	if err := insert(msg2, db); err != nil {
		t.Fatalf("insert msg2: %v", err)
	}

	got, err := getUnreadMessages("user-1", db)
	if err != nil {
		t.Fatalf("getUnreadMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
}

func TestMessage_markMessagesAsRead(t *testing.T) {
	db := newTestDB(t)
	msg1 := newMessage("user-2")
	msg2 := newMessage("user-2")

	if err := insert(msg1, db); err != nil {
		t.Fatalf("insert msg1: %v", err)
	}
	if err := insert(msg2, db); err != nil {
		t.Fatalf("insert msg2: %v", err)
	}

	if err := markMessagesAsRead([]string{msg1.ID, msg2.ID}, db); err != nil {
		t.Fatalf("markMessagesAsRead: %v", err)
	}

	got, err := getUnreadMessages("user-2", db)
	if err != nil {
		t.Fatalf("getUnreadMessages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(got))
	}
}

func TestMessage_updateStatus(t *testing.T) {
	db := newTestDB(t)
	msg := newMessage("user-3")

	if err := insert(msg, db); err != nil {
		t.Fatalf("insert msg: %v", err)
	}

	if err := updateStatus("user-3", db); err != nil {
		t.Fatalf("updateStatus: %v", err)
	}

	got, err := getUnreadMessages("user-3", db)
	if err != nil {
		t.Fatalf("getUnreadMessages: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(got))
	}
}

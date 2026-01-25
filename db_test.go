package chat

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := runMigrations(db, filepath.Join("cli", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func runMigrations(db *sqlx.DB, migrationsPath string) error {
	instance, err := sqlite3.WithInstance(db.DB, &sqlite3.Config{})
	if err != nil {
		return err
	}
	path, err := filepath.Abs(migrationsPath)
	if err != nil {
		return err
	}
	fSrc, err := (&file.File{}).Open(path)
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("file", fSrc, "sqlite3", instance)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
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

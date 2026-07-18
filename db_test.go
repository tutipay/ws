package chat

import (
	"context"
	"errors"
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
	t.Cleanup(func() { _ = db.Close() })
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

func testIdentity(tenant, user string) ClientIdentity {
	return ClientIdentity{TenantID: tenant, UserID: user}
}

func testMessage(tenant, from, to, text string) Message {
	return Message{
		TenantID: tenant, ID: uuid.NewString(), FromUserID: from, ToUserID: to,
		Text: text, Date: time.Now().UTC().Unix(), Type: FrameTypeMessage,
	}
}

func TestPersistence_TenantScopedUnreadAndDelivery(t *testing.T) {
	db := newTestDB(t)
	sharedID := uuid.NewString()
	alpha := testMessage("tenant-alpha", "sender", "same-user", "alpha")
	alpha.ID = sharedID
	beta := testMessage("tenant-beta", "sender", "same-user", "beta")
	beta.ID = sharedID

	if _, err := insert(alpha, db); err != nil {
		t.Fatalf("insert alpha: %v", err)
	}
	if _, err := insert(beta, db); err != nil {
		t.Fatalf("insert beta: %v", err)
	}

	alphaUnread, err := getUnreadMessages(testIdentity("tenant-alpha", "same-user"), 0, db)
	if err != nil {
		t.Fatalf("alpha unread: %v", err)
	}
	betaUnread, err := getUnreadMessages(testIdentity("tenant-beta", "same-user"), 0, db)
	if err != nil {
		t.Fatalf("beta unread: %v", err)
	}
	if len(alphaUnread) != 1 || alphaUnread[0].Text != "alpha" {
		t.Fatalf("alpha unread = %#v", alphaUnread)
	}
	if len(betaUnread) != 1 || betaUnread[0].Text != "beta" {
		t.Fatalf("beta unread = %#v", betaUnread)
	}

	if err := markMessagesDelivered(testIdentity("tenant-alpha", "same-user"), []string{sharedID}, db, 1); err != nil {
		t.Fatalf("mark alpha delivered: %v", err)
	}
	alphaUnread, _ = getUnreadMessages(testIdentity("tenant-alpha", "same-user"), 0, db)
	betaUnread, _ = getUnreadMessages(testIdentity("tenant-beta", "same-user"), 0, db)
	if len(alphaUnread) != 0 || len(betaUnread) != 1 {
		t.Fatalf("cross-tenant delivery mutation: alpha=%d beta=%d", len(alphaUnread), len(betaUnread))
	}
}

func TestPersistence_IdempotencyAndPayloadConflict(t *testing.T) {
	db := newTestDB(t)
	message := testMessage("tenant-alpha", "sender", "recipient", "hello")

	first, err := insert(message, db)
	if err != nil || !first.Created {
		t.Fatalf("first insert = %#v, %v", first, err)
	}
	retry := message
	retry.Date++
	second, err := insert(retry, db)
	if err != nil || second.Created || second.Message.Date != message.Date {
		t.Fatalf("exact retry = %#v, %v", second, err)
	}

	changed := message
	changed.Text = "changed"
	if _, err := insert(changed, db); !errors.Is(err, ErrMessageConflict) {
		t.Fatalf("changed retry error = %v, want %v", err, ErrMessageConflict)
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM chats_v2 WHERE tenant_id = ? AND id = ?`, message.TenantID, message.ID); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
}

func TestContacts_AreStableAndTenantScoped(t *testing.T) {
	db := newTestDB(t)
	alphaOwner := testIdentity("tenant-alpha", "owner")
	betaOwner := testIdentity("tenant-beta", "owner")
	if err := AddContacts(context.Background(), alphaOwner, []string{"shared-contact", "shared-contact", "owner"}, db); err != nil {
		t.Fatalf("add alpha contacts: %v", err)
	}
	if err := AddContacts(context.Background(), betaOwner, []string{"shared-contact"}, db); err != nil {
		t.Fatalf("add beta contacts: %v", err)
	}

	alpha, err := getContactOwners(testIdentity("tenant-alpha", "shared-contact"), db)
	if err != nil {
		t.Fatalf("alpha contact owners: %v", err)
	}
	beta, err := getContactOwners(testIdentity("tenant-beta", "shared-contact"), db)
	if err != nil {
		t.Fatalf("beta contact owners: %v", err)
	}
	if len(alpha) != 1 || alpha[0] != alphaOwner || len(beta) != 1 || beta[0] != betaOwner {
		t.Fatalf("owners crossed tenants: alpha=%#v beta=%#v", alpha, beta)
	}
}

func TestPersistence_RejectsMissingDatabase(t *testing.T) {
	message := testMessage("tenant", "sender", "recipient", "hello")
	if _, err := insert(message, nil); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("insert nil error = %v", err)
	}
	if err := markMessagesDelivered(testIdentity("tenant", "recipient"), []string{message.ID}, nil, 1); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("mark nil error = %v", err)
	}
}

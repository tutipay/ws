package chat

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
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

func testIdentity(tenant string, user int64) ClientIdentity {
	return ClientIdentity{TenantID: tenant, UserID: user}
}

func testMessage(tenant string, from, to int64, text string) Message {
	return Message{
		TenantID: tenant, ID: uuid.NewString(), FromUserID: from, ToUserID: to,
		Text: text, Date: time.Now().UTC().Unix(), Type: FrameTypeMessage,
	}
}

func TestPersistence_TenantScopedUnreadAndDelivery(t *testing.T) {
	db := newTestDB(t)
	sharedID := uuid.NewString()
	alpha := testMessage("tenant-alpha", 1, 2, "alpha")
	alpha.ID = sharedID
	beta := testMessage("tenant-beta", 1, 2, "beta")
	beta.ID = sharedID

	if _, err := insert(alpha, db); err != nil {
		t.Fatalf("insert alpha: %v", err)
	}
	if _, err := insert(beta, db); err != nil {
		t.Fatalf("insert beta: %v", err)
	}

	alphaUnread, err := getUnreadMessages(testIdentity("tenant-alpha", 2), 0, db)
	if err != nil {
		t.Fatalf("alpha unread: %v", err)
	}
	betaUnread, err := getUnreadMessages(testIdentity("tenant-beta", 2), 0, db)
	if err != nil {
		t.Fatalf("beta unread: %v", err)
	}
	if len(alphaUnread) != 1 || alphaUnread[0].Text != "alpha" {
		t.Fatalf("alpha unread = %#v", alphaUnread)
	}
	if len(betaUnread) != 1 || betaUnread[0].Text != "beta" {
		t.Fatalf("beta unread = %#v", betaUnread)
	}

	if _, err := markMessagesDelivered(testIdentity("tenant-alpha", 2), []string{sharedID}, db, 1); err != nil {
		t.Fatalf("mark alpha delivered: %v", err)
	}
	alphaUnread, _ = getUnreadMessages(testIdentity("tenant-alpha", 2), 0, db)
	betaUnread, _ = getUnreadMessages(testIdentity("tenant-beta", 2), 0, db)
	if len(alphaUnread) != 0 || len(betaUnread) != 1 {
		t.Fatalf("cross-tenant delivery mutation: alpha=%d beta=%d", len(alphaUnread), len(betaUnread))
	}
	foreignAck, err := markMessagesDelivered(testIdentity("tenant-beta", 3), []string{sharedID}, db, 1)
	if err != nil {
		t.Fatalf("mark foreign recipient delivered: %v", err)
	}
	if len(foreignAck) != 0 {
		t.Fatalf("foreign recipient was falsely acknowledged: %#v", foreignAck)
	}
	betaUnread, _ = getUnreadMessages(testIdentity("tenant-beta", 2), 0, db)
	if len(betaUnread) != 1 {
		t.Fatalf("foreign recipient marked message delivered: %#v", betaUnread)
	}
}

func TestPersistence_IdempotencyAndPayloadConflict(t *testing.T) {
	db := newTestDB(t)
	message := testMessage("tenant-alpha", 1, 2, "hello")

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

	changedText := message
	changedText.Text = "changed"
	changedRecipient := message
	changedRecipient.ToUserID = 3
	changedSender := message
	changedSender.FromUserID = 4
	for _, changed := range []Message{changedText, changedRecipient, changedSender} {
		if _, err := insert(changed, db); !errors.Is(err, ErrMessageConflict) {
			t.Fatalf("changed retry %#v error = %v, want %v", changed, err, ErrMessageConflict)
		}
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM chats_v2 WHERE tenant_id = ? AND id = ?`, message.TenantID, message.ID); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count = %d, want 1", count)
	}
}

func TestPersistence_ConcurrentExactRetryCreatesOneRow(t *testing.T) {
	db := newTestDB(t)
	message := testMessage("tenant", 1, 2, "hello")
	const callers = 16
	start := make(chan struct{})
	var created atomic.Int32
	errorsSeen := make(chan error, callers)
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := insert(message, db)
			if err == nil && result.Created {
				created.Add(1)
			}
			errorsSeen <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent insert: %v", err)
		}
	}
	if created.Load() != 1 {
		t.Fatalf("created count = %d, want 1", created.Load())
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM chats_v2 WHERE tenant_id = ? AND id = ?`, message.TenantID, message.ID); err != nil || count != 1 {
		t.Fatalf("persisted count = %d, error = %v", count, err)
	}
}

func TestContacts_AreStableAndTenantScoped(t *testing.T) {
	db := newTestDB(t)
	alphaOwner := testIdentity("tenant-alpha", 1)
	betaOwner := testIdentity("tenant-beta", 1)
	if err := AddContacts(context.Background(), alphaOwner, []int64{2, 2, 1}, db); err != nil {
		t.Fatalf("add alpha contacts: %v", err)
	}
	if err := AddContacts(context.Background(), betaOwner, []int64{2}, db); err != nil {
		t.Fatalf("add beta contacts: %v", err)
	}

	alpha, err := getContactOwners(testIdentity("tenant-alpha", 2), db)
	if err != nil {
		t.Fatalf("alpha contact owners: %v", err)
	}
	beta, err := getContactOwners(testIdentity("tenant-beta", 2), db)
	if err != nil {
		t.Fatalf("beta contact owners: %v", err)
	}
	if len(alpha) != 1 || alpha[0] != alphaOwner || len(beta) != 1 || beta[0] != betaOwner {
		t.Fatalf("owners crossed tenants: alpha=%#v beta=%#v", alpha, beta)
	}
}

func TestContacts_RejectInvalidBatchShapes(t *testing.T) {
	db := newTestDB(t)
	owner := testIdentity("tenant", 1)
	tooMany := make([]int64, maxContactsPerRequest+1)
	for index := range tooMany {
		tooMany[index] = int64(index + 2)
	}
	for name, contacts := range map[string][]int64{
		"empty":     nil,
		"too many":  tooMany,
		"zero":      {0},
		"negative":  {-1},
		"self only": {1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := AddContacts(context.Background(), owner, contacts, db); !errors.Is(err, ErrInvalidContactBatch) {
				t.Fatalf("AddContacts(%v) error = %v", contacts, err)
			}
		})
	}
	var count int
	if err := db.Get(&count, `SELECT count(*) FROM contacts_v2`); err != nil || count != 0 {
		t.Fatalf("invalid contacts persisted: count=%d error=%v", count, err)
	}
}

func TestPersistence_RejectsMissingDatabase(t *testing.T) {
	message := testMessage("tenant", 1, 2, "hello")
	if _, err := insert(message, nil); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("insert nil error = %v", err)
	}
	if _, err := markMessagesDelivered(testIdentity("tenant", 2), []string{message.ID}, nil, 1); !errors.Is(err, ErrPersistenceUnavailable) {
		t.Fatalf("mark nil error = %v", err)
	}
}

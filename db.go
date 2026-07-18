package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var (
	ErrMessageConflict        = errors.New("message id conflicts with persisted payload")
	ErrPersistenceUnavailable = errors.New("chat persistence unavailable")
	ErrInvalidContactBatch    = errors.New("invalid contact batch")
)

const maxContactsPerRequest = 50

type persistResult struct {
	Message Message
	Created bool
}

func insert(message Message, db *sqlx.DB) (persistResult, error) {
	if db == nil {
		return persistResult{}, ErrPersistenceUnavailable
	}
	tx, err := db.Beginx()
	if err != nil {
		return persistResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.NamedExec(`
		INSERT INTO chats_v2
			(tenant_id, id, from_user_id, to_user_id, text, is_delivered, date)
		VALUES
			(:tenant_id, :id, :from_user_id, :to_user_id, :text, :is_delivered, :date)
		ON CONFLICT (tenant_id, id) DO NOTHING`, message)
	if err != nil {
		return persistResult{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return persistResult{}, err
	}
	if rows == 1 {
		if err := tx.Commit(); err != nil {
			return persistResult{}, err
		}
		return persistResult{Message: message, Created: true}, nil
	}

	var persisted Message
	query := tx.Rebind(`
		SELECT tenant_id, id, from_user_id, to_user_id, text, is_delivered, date
		FROM chats_v2
		WHERE tenant_id = ? AND id = ?`)
	if err := tx.Get(&persisted, query, message.TenantID, message.ID); err != nil {
		return persistResult{}, err
	}
	if persisted.FromUserID != message.FromUserID ||
		persisted.ToUserID != message.ToUserID ||
		persisted.Text != message.Text {
		return persistResult{}, ErrMessageConflict
	}
	if err := tx.Commit(); err != nil {
		return persistResult{}, err
	}
	return persistResult{Message: persisted}, nil
}

func getUnreadMessages(identity ClientIdentity, limit int, db *sqlx.DB) ([]Message, error) {
	if db == nil {
		return nil, ErrPersistenceUnavailable
	}
	if err := identity.validate(); err != nil {
		return nil, err
	}
	query := `
		SELECT tenant_id, id, from_user_id, to_user_id, text, is_delivered, date
		FROM chats_v2
		WHERE tenant_id = ? AND to_user_id = ? AND is_delivered = FALSE
		ORDER BY date, id`
	args := []any{identity.TenantID, identity.UserID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	var messages []Message
	if err := db.Select(&messages, db.Rebind(query), args...); err != nil {
		return nil, err
	}
	for index := range messages {
		messages[index].Type = FrameTypeMessage
	}
	return messages, nil
}

func markMessagesDelivered(identity ClientIdentity, messageIDs []string, db *sqlx.DB, batchSize int) ([]string, error) {
	if db == nil {
		return nil, ErrPersistenceUnavailable
	}
	if err := identity.validate(); err != nil {
		return nil, err
	}
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if batchSize <= 0 {
		batchSize = len(messageIDs)
	}
	delivered := make([]string, 0, len(messageIDs))
	for start := 0; start < len(messageIDs); start += batchSize {
		end := start + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		batch := messageIDs[start:end]
		selectQuery, selectArgs, err := sqlx.In(`
			SELECT id
			FROM chats_v2
			WHERE tenant_id = ? AND to_user_id = ? AND id IN (?)`,
			identity.TenantID, identity.UserID, batch)
		if err != nil {
			return nil, err
		}
		var owned []string
		if err := db.Select(&owned, db.Rebind(selectQuery), selectArgs...); err != nil {
			return nil, err
		}
		ownedSet := make(map[string]struct{}, len(owned))
		for _, id := range owned {
			ownedSet[id] = struct{}{}
		}
		for _, id := range batch {
			if _, ok := ownedSet[id]; ok {
				delivered = append(delivered, id)
			}
		}
		if len(owned) == 0 {
			continue
		}
		updateQuery, updateArgs, err := sqlx.In(`
			UPDATE chats_v2
			SET is_delivered = TRUE
			WHERE tenant_id = ? AND to_user_id = ? AND id IN (?)`,
			identity.TenantID, identity.UserID, owned)
		if err != nil {
			return nil, err
		}
		if _, err := db.Exec(db.Rebind(updateQuery), updateArgs...); err != nil {
			return nil, err
		}
	}
	return delivered, nil
}

// AddContacts stores directed, tenant-scoped contact relationships. Identity
// resolution belongs to the embedding application; this package accepts only
// stable user IDs and never queries or mirrors an identity user table.
func AddContacts(ctx context.Context, owner ClientIdentity, contactUserIDs []int64, db *sqlx.DB) error {
	if db == nil {
		return ErrPersistenceUnavailable
	}
	if err := owner.validate(); err != nil {
		return err
	}
	if len(contactUserIDs) == 0 || len(contactUserIDs) > maxContactsPerRequest {
		return ErrInvalidContactBatch
	}
	unique := make([]int64, 0, len(contactUserIDs))
	seen := make(map[int64]struct{}, len(contactUserIDs))
	for _, userID := range contactUserIDs {
		if userID <= 0 {
			return fmt.Errorf("%w: contact user id", ErrInvalidContactBatch)
		}
		if userID == owner.UserID {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}
	if len(unique) == 0 {
		return ErrInvalidContactBatch
	}
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := tx.Rebind(`
		INSERT INTO contacts_v2 (tenant_id, owner_user_id, contact_user_id)
		VALUES (?, ?, ?)
		ON CONFLICT (tenant_id, owner_user_id, contact_user_id) DO NOTHING`)
	for _, userID := range unique {
		if _, err := tx.ExecContext(ctx, query, owner.TenantID, owner.UserID, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func getContactOwners(identity ClientIdentity, db *sqlx.DB) ([]ClientIdentity, error) {
	if db == nil {
		return nil, ErrPersistenceUnavailable
	}
	if err := identity.validate(); err != nil {
		return nil, err
	}
	query := db.Rebind(`
		SELECT owner_user_id
		FROM contacts_v2
		WHERE tenant_id = ? AND contact_user_id = ?
		ORDER BY owner_user_id`)
	var userIDs []int64
	if err := db.Select(&userIDs, query, identity.TenantID, identity.UserID); err != nil {
		return nil, err
	}
	owners := make([]ClientIdentity, 0, len(userIDs))
	for _, userID := range userIDs {
		owners = append(owners, ClientIdentity{TenantID: identity.TenantID, UserID: userID})
	}
	return owners, nil
}

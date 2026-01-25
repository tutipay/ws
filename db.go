package chat

import (
	"errors"
	"log"

	"github.com/jmoiron/sqlx"
)

// Message represents a table of all chat messages that are stored in
// ws package.
// We rely on the consumer of the package to provide us with their own database connection client
// since it doesn't make much since to do that for them.
type Message struct {
	ID          string `db:"id" json:"id,omitempty"`
	From        string `db:"from" json:"from,omitempty"`
	To          string `db:"to" json:"to,omitempty"`
	Text        string `db:"text" json:"text,omitempty"`
	IsDelivered bool   `db:"is_delivered" json:"is_delivered,omitempty"`
	Date        int64  `db:"date" json:"date"`
	Type        string `db:"-" json:"type,omitempty"`
	IsTyping    *bool  `db:"-" json:"is_typing,omitempty"`
}

// Contact type represents a relationship between two clients, the relationship reads:
// `Second` client is a contact of the `First` client, not vice verca. Because person A can have person B
// in their contact list, but person B may not have A in their contact list. And we don't want to bother B
// with notifications that A is connected if they don't have A in their contact list.
type Contact struct {
	First  string // First client ID
	Second string // Second client ID

	// Both is the concat of First+Second and its is used in uniquely identifying the pair.
	Both string
}

func insert(msg Message, db *sqlx.DB) error {
	return insertBatch([]Message{msg}, db)
}

func insertBatch(messages []Message, db *sqlx.DB) error {
	if db == nil || len(messages) == 0 {
		return nil
	}
	tx, err := db.Beginx()
	if err != nil {
		log.Printf("insertBatch begin error: %v", err)
		return err
	}
	for _, msg := range messages {
		if _, err := tx.NamedExec(`INSERT into chats("id", "from", "to", "text", "is_delivered", "date") values(:id, :from, :to, :text, :is_delivered, :date)`, msg); err != nil {
			_ = tx.Rollback()
			log.Printf("insertBatch exec error: %v", err)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("insertBatch commit error: %v", err)
		return err
	}
	return nil
}

func updateStatus(mobile string, db *sqlx.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "to" = $1`, mobile); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func getUnreadMessages(mobile string, limit int, db *sqlx.DB) ([]Message, error) {
	if db == nil {
		return nil, nil
	}
	query := `SELECT * from chats where "to" = ? and is_delivered = 0 order by date`
	args := []any{mobile}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	query = db.Rebind(query)
	var chats []Message
	if err := db.Select(&chats, query, args...); err != nil {
		return nil, err
	}
	return chats, nil
}

func markMessageAsRead(messageID string, db *sqlx.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "id" = $1`, messageID); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func markMessagesAsRead(messageIDs []string, db *sqlx.DB, batchSize int) error {
	if db == nil || len(messageIDs) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(messageIDs)
	}
	for start := 0; start < len(messageIDs); start += batchSize {
		end := start + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		batch := messageIDs[start:end]
		query, args, err := sqlx.In(`Update chats set is_delivered = 1 where "id" IN (?)`, batch)
		if err != nil {
			log.Printf("the error is: %v", err)
			return err
		}
		query = db.Rebind(query)
		if _, err := db.Exec(query, args...); err != nil {
			log.Printf("the error is: %v", err)
			return err
		}
	}
	return nil
}

// addContactsToDB adds a "contact of" relationship between a submitted contact
// and the currentUser It is important to note that this relationship is not
// bidirectional, i.e. if A is a contact of B that does not mean B is also a
// contact of A.
//
// Returns which of these contacts are registered users in the database
func addContactsToDB(currentUser string, contacts []ContactsRequest, db *sqlx.DB) ([]ContactsRequest, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}

	var user User
	var contactsThatAreUsers []ContactsRequest

	for _, contact := range contacts {
		row := db.QueryRow(`SELECT "fullname", "mobile" from users where "mobile" = ?`, contact.Mobile)
		if err := row.Scan(&user.Name, &user.Mobile); err != nil {
			log.Printf("Error in query: %v", err)
			continue
		}
		contactsThatAreUsers = append(contactsThatAreUsers, contact)

		// This is done to prevent duplication by checking if the record already exists
		both := currentUser + user.Mobile
		var resultOfBothQuery string

		row = db.QueryRow(`SELECT "both" from contacts where "both" = ?`, both)
		if err := row.Scan(&resultOfBothQuery); err != nil {
			log.Printf("%v -> Record can be inserted", err)
			if _, err := db.Exec(`INSERT into contacts("first", "second", "both") values($1, $2, $3)`, currentUser, user.Mobile, currentUser+user.Mobile); err != nil {
				log.Printf("Error inserting contact: %v", err)
				return nil, err
			}
		} else {
			log.Printf("Record already exists: %v", resultOfBothQuery)
		}
	}
	return contactsThatAreUsers, nil
}

// getContacts returns a list of user IDs (phone numbers) that have the user with the ID `clientID`
// as their contact
func getContacts(clientID string, db *sqlx.DB) ([]string, error) {
	if db == nil {
		return nil, nil
	}
	var contacts []string
	if err := db.Select(&contacts, `SELECT "first" from contacts where "second" = $1`, clientID); err != nil {
		log.Printf("Error retrieving contacts: %v", err)
		return nil, err
	}
	return contacts, nil
}

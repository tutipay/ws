package chat

import (
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var stmt = `CREATE TABLE IF NOT EXISTS "chats" (
	"id"	TEXT,
	"from"	INTEGER,
	"to"	INTEGER,
	"text"	TEXT,
	"is_delivered"	INTEGER DEFAULT 0,
	"date"  INTEGER,
	PRIMARY KEY("id")
);`

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
}

func OpenDb(name string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("sqlite3", name)
	if err != nil {
		log.Printf("error in opening db: %v", err)
		return nil, err
	}
	db.MustExec(stmt)
	return db, nil
}

func insert(msg Message, db *sqlx.DB) error {
	if _, err := db.NamedExec(`INSERT into chats("from", "to", "text") values(:from, :to, :text)`, msg); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func updateStatus(mobile string, db *sqlx.DB) error {
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "to" = $1`, mobile); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func getUnreadMessages(mobile string, db *sqlx.DB) ([]Message, error) {
	var chats []Message
	if err := db.Select(&chats, `SELECT * from chats where "to" = $1 and is_delivered = 0 order by id`, mobile); err != nil {
		return nil, err
	}
	return chats, nil
}

func markMessageAsRead(messageID string, db *sqlx.DB) error {
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "id" = $1`, messageID); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

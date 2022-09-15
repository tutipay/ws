package chat

import (
	"errors"
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var stmt = `CREATE TABLE IF NOT EXISTS "chats" (
	"id"	INTEGER,
	"from"	INTEGER,
	"to"	INTEGER,
	"text"	TEXT,
	"is_delivered"	INTEGER DEFAULT 0,
	PRIMARY KEY("id" AUTOINCREMENT)
);`

// Message represents a table of all chat messages that are stored in
// ws package.
// We rely on the consumer of the package to provide us with their own database connection client
// since it doesn't make much since to do that for them.
type Message struct {
	ID          int    `db:"id"`
	From        string `db:"from"`
	To          string `db:"to"`
	Text        string `db:"text"`
	IsDelivered bool   `db:"is_delivered"`
	db          *sqlx.DB
}

// NewMessage creates a new instance of a Message populated with a database
func NewMessage(db *sqlx.DB) (Message, error) {
	if db == nil {
		return Message{}, errors.New("the db client is nil")
	}
	return Message{db: db}, nil
}

func openDb() (*sqlx.DB, error) {
	db, err := sqlx.Connect("sqlite3", "test.db")
	if err != nil {
		log.Printf("error in opening db: %v", err)
		return nil, err
	}
	db.MustExec(stmt)
	return db, nil
}

func (c Message) insert(db *sqlx.DB) error {
	if _, err := c.db.NamedExec(`INSERT into chats("from", "to", "text") values(:from, :to, :text)`, c); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func (m Message) readAll(mobile string, db *sqlx.DB) error {
	if _, err := db.Exec(`Update chats set is_delivered = 1 where "to" = $1`, mobile); err != nil {
		log.Printf("the error is: %v", err)
		return err
	}
	return nil
}

func (m Message) getUnreadMessages(mobile string) ([]Message, error) {
	var chats []Message
	if err := m.db.Select(&chats, `SELECT * from chats where "to" = $1 and is_delivered = 0 order by id`, mobile); err != nil {
		return nil, err
	}
	return chats, nil
}

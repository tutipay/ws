package chat

import (
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
type Message struct {
	ID          int    `db:"id"`
	From        string `db:"from"`
	To          string `db:"to"`
	Text        string `db:"text"`
	IsDelivered bool   `db:"is_delivered"`
	db          *sqlx.DB
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

func getUnreadMessages(mobile string, db *sqlx.DB) ([]Message, error) {
	var chats []Message
	if err := db.Select(&chats, `SELECT * from chats where "to" = $1 and is_delivered = 0 order by id`, mobile); err != nil {
		return nil, err
	}
	return chats, nil
}

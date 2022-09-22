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
	"date"  INTEGER,
	PRIMARY KEY("id" AUTOINCREMENT)
);`

// Message represents a table of all chat messages that are stored in
// ws package.
// We rely on the consumer of the package to provide us with their own database connection client
// since it doesn't make much since to do that for them.
type Message struct {
	ID          int    `db:"id" json:"id,omitempty"`
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

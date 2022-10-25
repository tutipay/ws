package chat

import (
	"log"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var stmt = `CREATE TABLE IF NOT EXISTS "chats" (
	"id"	TEXT,
	"from"	TEXT,
	"to"	TEXT,
	"text"	TEXT,
	"is_delivered"	INTEGER DEFAULT 0,
	"date"  INTEGER,
	PRIMARY KEY("id")
);`

var stmtContacts = `CREATE TABLE IF NOT EXISTS "contacts" (
	first  TEXT,
	second TEXT,
	both   TEXT,
	FOREIGN KEY(first) REFERENCES users(mobile),
	FOREIGN KEY(second) REFERENCES users(mobile)
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

func OpenDb(name string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("sqlite3", name)
	if err != nil {
		log.Printf("error in opening db: %v", err)
		return nil, err
	}
	db.MustExec(stmt)
	db.MustExec(stmtContacts)
	return db, nil
}

func insert(msg Message, db *sqlx.DB) error {
	if _, err := db.NamedExec(`INSERT into chats("id", "from", "to", "text", "is_delivered", "date") values(:id, :from, :to, :text, :is_delivered, :date)`, msg); err != nil {
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
	if err := db.Select(&chats, `SELECT * from chats where "to" = $1 and is_delivered = 0 order by date`, mobile); err != nil {
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

func addContactsToDB(currentUser string, contacts []ContactsRequest, db *sqlx.DB) error {

	var user User

	for _, contact := range contacts {
		row := db.QueryRow(`SELECT "fullname", "mobile" from users where "mobile" = ?`, contact.Mobile)
		if err := row.Scan(&user.Name, &user.Mobile); err != nil {
			log.Printf("Error in query: %v", err)
			continue
		}

		// This is done to prevent duplication by checking if the record already exists
		both := currentUser + user.Mobile
		var resultOfBothQuery string

		row = db.QueryRow(`SELECT "both" from contacts where "both" = ?`, both)
		if err := row.Scan(&resultOfBothQuery); err != nil {
			log.Printf("%v -> Record can be inserted", err)
			if _, err := db.Exec(`INSERT into contacts("first", "second", "both") values($1, $2, $3)`, currentUser, user.Mobile, currentUser+user.Mobile); err != nil {
				log.Printf("Error inserting contact: %v", err)
				return err
			}
		} else {
			log.Printf("Record already exists: %v", resultOfBothQuery)
		}
	}
	return nil
}

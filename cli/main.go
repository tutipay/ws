package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	chat "github.com/tutipay/ws"
)

func serveHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func main() {
	db, err := sqlx.Connect("sqlite3", "test.db")
	if err != nil {
		log.Fatalf("the data is null: %v", err)
	}
	if err := runMigrations(db.DB, "./migrations"); err != nil {
		log.Fatalf("error in migrating: %v", err)
	}
	defer db.Close()
	cfg := chat.DefaultHubConfig()
	cfg.ClientIdentityFromRequest = func(r *http.Request) (chat.ClientIdentity, error) {
		return chat.ClientIdentity{
			TenantID: r.URL.Query().Get("tenantID"),
			UserID:   r.URL.Query().Get("userID"),
		}, nil
	}
	hub := chat.NewHubWithConfig(db, cfg)
	go hub.Run()
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveHome)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		chat.ServeWs(hub, w, r)
	})

	// This is only for testing it's not used in production
	mux.HandleFunc("/submitContacts", func(w http.ResponseWriter, r *http.Request) {
		chat.SubmitContacts(chat.ClientIdentity{TenantID: "demo", UserID: "user-a"}, db, w, r)
	})

	log.Fatal(http.ListenAndServe(":6446", mux))
}

func runMigrations(db *sql.DB, dir string) error {
	instance, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		return err
	}

	fSrc, err := (&file.File{}).Open(dir)
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

package main

import (
	"database/sql"
	"log"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/file"
)

func TestMigrationTable(t *testing.T) {

	db, err := sql.Open("sqlite3", "./test.db")
	if err != nil {
		log.Fatalf("error in open db: %v", err)
	}
	defer db.Close()

	instance, err := sqlite3.WithInstance(db, &sqlite3.Config{})
	if err != nil {
		log.Fatalf("error in sqlite3 with instance: %v", err)
	}

	fSrc, err := (&file.File{}).Open("./migrations")
	if err != nil {
		log.Fatalf("error in migrations dir: %v", err)
	}

	m, err := migrate.NewWithInstance("file", fSrc, "sqlite3", instance)
	if err != nil {
		log.Fatalf("error in instance: %v", err)
	}

	// modify for Down
	if err := m.Up(); err != nil {
		log.Fatalf("error in migrating: %v", err)
	}
}

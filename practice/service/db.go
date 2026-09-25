package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS items (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	name      TEXT    NOT NULL,
	stock     INTEGER NOT NULL CHECK (stock >= 0),
	available INTEGER NOT NULL CHECK (available >= 0 AND available <= stock)
);

CREATE TABLE IF NOT EXISTS reservations (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	item_id  INTEGER NOT NULL REFERENCES items(id),
	quantity INTEGER NOT NULL CHECK (quantity >= 1),
	status   TEXT    NOT NULL CHECK (status IN ('active', 'cancelled'))
);
`

// openDB opens the SQLite database at path and creates the schema.
// When fresh is true any existing database file is removed first, so every
// container start begins with an empty database (C-RUN-3).
func openDB(path string, fresh bool) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if fresh {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("remove old database: %w", err)
			}
		}
	}

	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serialises every statement, so check-and-write sequences
	// in a transaction cannot interleave and SQLITE_BUSY never surfaces.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return db, nil
}

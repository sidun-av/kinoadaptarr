// Package cache persists resolved (Cyrillic title -> English title)
// mappings so repeat lookups for the same show don't re-query Kinopoisk or
// TMDB, keeping well within their rate limits.
package cache

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Mapping is a resolved title translation.
type Mapping struct {
	EnglishTitle string
	TMDBID       int
}

// Cache is a SQLite-backed store of resolved title mappings, keyed by the
// Cyrillic title segment that was looked up.
type Cache struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path and
// ensures its schema exists.
func Open(path string) (*Cache, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS title_mappings (
			cyrillic_key  TEXT PRIMARY KEY,
			english_title TEXT NOT NULL,
			tmdb_id       INTEGER NOT NULL,
			resolved_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Cache{db: db}, nil
}

// Close closes the underlying database connection.
func (c *Cache) Close() error {
	return c.db.Close()
}

// Get returns the cached mapping for key, if one exists.
func (c *Cache) Get(key string) (*Mapping, bool, error) {
	var m Mapping
	err := c.db.QueryRow(
		`SELECT english_title, tmdb_id FROM title_mappings WHERE cyrillic_key = ?`,
		key,
	).Scan(&m.EnglishTitle, &m.TMDBID)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("query mapping: %w", err)
	}
	return &m, true, nil
}

// Put stores (or overwrites) the mapping for key.
func (c *Cache) Put(key string, m Mapping) error {
	_, err := c.db.Exec(`
		INSERT INTO title_mappings (cyrillic_key, english_title, tmdb_id)
		VALUES (?, ?, ?)
		ON CONFLICT(cyrillic_key) DO UPDATE SET
			english_title = excluded.english_title,
			tmdb_id = excluded.tmdb_id,
			resolved_at = CURRENT_TIMESTAMP
	`, key, m.EnglishTitle, m.TMDBID)
	if err != nil {
		return fmt.Errorf("insert mapping: %w", err)
	}
	return nil
}

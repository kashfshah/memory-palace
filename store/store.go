package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Record represents a single memory entry from any source.
type Record struct {
	Source    string
	Timestamp time.Time
	Title    string
	URL      string
	Body     string
	Location string
	RawID    string
}

// IndexStats holds summary statistics for the memory index.
type IndexStats struct {
	Total  int
	BySrc  map[string]int
	Oldest time.Time
	Newest time.Time
	Built  time.Time
}

// DB wraps the unified memory database.
type DB struct {
	conn *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS memory (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    title TEXT,
    url TEXT,
    body TEXT,
    summary TEXT,
    location TEXT,
    raw_id TEXT,
    UNIQUE(source, raw_id)
);

CREATE INDEX IF NOT EXISTS idx_memory_source ON memory(source);
CREATE INDEX IF NOT EXISTS idx_memory_timestamp ON memory(timestamp);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    title, url, body, summary
);

CREATE TABLE IF NOT EXISTS meta (
    key TEXT PRIMARY KEY,
    value TEXT
);
`

// Open creates or opens the memory database at the given path.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Upsert inserts or replaces records for a given source. Returns count inserted.
func (db *DB) Upsert(source string, records []Record) (int, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Clear existing records for this source
	if _, err := tx.Exec("DELETE FROM memory WHERE source = ?", source); err != nil {
		return 0, fmt.Errorf("clear %s: %w", source, err)
	}

	stmt, err := tx.Prepare(`INSERT INTO memory (source, timestamp, title, url, body, location, raw_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for _, r := range records {
		_, err := stmt.Exec(source, r.Timestamp.Unix(), r.Title, r.URL, r.Body, r.Location, r.RawID)
		if err != nil {
			continue // skip bad records
		}
		count++
	}

	return count, tx.Commit()
}

// RebuildFTS rebuilds the full-text search index from the memory table.
func (db *DB) RebuildFTS() error {
	if _, err := db.conn.Exec("DELETE FROM memory_fts"); err != nil {
		return err
	}
	_, err := db.conn.Exec(`INSERT INTO memory_fts(rowid, title, url, body, summary)
		SELECT id, COALESCE(title,''), COALESCE(url,''), COALESCE(body,''), COALESCE(summary,'') FROM memory`)
	return err
}

// DeleteBlocked removes records whose URL or title matches the provided blocked
// domains and title substrings. Used for immediate on-demand sanitization of
// records already in the DB (complementing the per-source upsert sanitization).
// Returns the count of deleted records.
func (db *DB) DeleteBlocked(blockedDomains []string, titleSubstrings []string) (int, error) {
	total := 0

	// Delete by domain: fetch all distinct URLs and check each against the list.
	// We do this in Go rather than SQL because hostname parsing requires net/url.
	rows, err := db.conn.Query("SELECT DISTINCT id, url FROM memory WHERE url IS NOT NULL AND url != ''")
	if err != nil {
		return 0, fmt.Errorf("query urls: %w", err)
	}
	type row struct {
		id  int64
		url string
	}
	var toDelete []int64
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.url); err != nil {
			continue
		}
		if isBlockedURL(r.url, blockedDomains) {
			toDelete = append(toDelete, r.id)
		}
	}
	rows.Close()

	if len(toDelete) > 0 {
		tx, err := db.conn.Begin()
		if err != nil {
			return 0, err
		}
		stmt, err := tx.Prepare("DELETE FROM memory WHERE id = ?")
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		for _, id := range toDelete {
			if _, err := stmt.Exec(id); err == nil {
				total++
			}
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			return total, err
		}
	}

	// Delete by title keywords using SQL LIKE (fast, no parsing needed).
	for _, sub := range titleSubstrings {
		res, err := db.conn.Exec("DELETE FROM memory WHERE LOWER(COALESCE(title,'')) LIKE ?",
			"%"+strings.ToLower(sub)+"%")
		if err != nil {
			continue
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}

	return total, nil
}

// isBlockedURL checks whether a raw URL matches any blocked domain (hostname or decoded full URL).
func isBlockedURL(rawURL string, blockedDomains []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range blockedDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	decoded, _ := url.QueryUnescape(rawURL)
	lower := strings.ToLower(decoded)
	for _, d := range blockedDomains {
		if strings.Contains(lower, d) {
			return true
		}
	}
	return false
}

// SetMeta writes a key-value pair to the meta table.
func (db *DB) SetMeta(key, value string) error {
	_, err := db.conn.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, key, value)
	return err
}

// EnrichCandidate represents a URL worth summarizing.
type EnrichCandidate struct {
	ID    int64
	URL   string
	Title string
}

// GetUnenrichedURLs returns URLs that have no summary yet, prioritized by
// bookmarks first, then most-visited history items.
func (db *DB) GetUnenrichedURLs(limit int) ([]EnrichCandidate, error) {
	rows, err := db.conn.Query(`
		SELECT id, url, COALESCE(title,'')
		FROM memory
		WHERE url != '' AND url IS NOT NULL
			AND (summary IS NULL OR summary = '')
			AND url LIKE 'http%'
		ORDER BY
			CASE source
				WHEN 'safari_bookmarks' THEN 0
				WHEN 'notes' THEN 1
				ELSE 2
			END,
			timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []EnrichCandidate
	for rows.Next() {
		var c EnrichCandidate
		if err := rows.Scan(&c.ID, &c.URL, &c.Title); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// SetSummary updates the summary field for a given record ID.
func (db *DB) SetSummary(id int64, summary string) error {
	_, err := db.conn.Exec("UPDATE memory SET summary = ? WHERE id = ?", summary, id)
	return err
}

// EnrichStats returns counts of enriched vs unenriched URL records.
func (db *DB) EnrichStats() (enriched, total int, err error) {
	err = db.conn.QueryRow(`
		SELECT
			COUNT(CASE WHEN summary IS NOT NULL AND summary != '' THEN 1 END),
			COUNT(*)
		FROM memory WHERE url != '' AND url IS NOT NULL AND url LIKE 'http%'
	`).Scan(&enriched, &total)
	return
}

// Query runs a full-text search and returns matching records.
func Query(dbPath, query string) ([]Record, error) {
	conn, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT m.source, m.timestamp, m.title, m.url, m.body, m.location
		FROM memory m
		JOIN memory_fts f ON f.rowid = m.id
		WHERE memory_fts MATCH ?
		ORDER BY f.rank
		LIMIT 50
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Record
	for rows.Next() {
		var r Record
		var ts int64
		var body, loc sql.NullString
		if err := rows.Scan(&r.Source, &ts, &r.Title, &r.URL, &body, &loc); err != nil {
			continue
		}
		r.Timestamp = time.Unix(ts, 0)
		r.Body = body.String
		r.Location = loc.String
		results = append(results, r)
	}
	return results, nil
}

// Stats returns summary statistics for the memory index.
func Stats(dbPath string) (*IndexStats, error) {
	conn, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	s := &IndexStats{BySrc: make(map[string]int)}

	// Total
	conn.QueryRow("SELECT COUNT(*) FROM memory").Scan(&s.Total)

	// By source
	rows, err := conn.Query("SELECT source, COUNT(*) FROM memory GROUP BY source")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src string
		var count int
		rows.Scan(&src, &count)
		s.BySrc[src] = count
	}

	// Date range
	var oldest, newest int64
	conn.QueryRow("SELECT COALESCE(MIN(timestamp),0), COALESCE(MAX(timestamp),0) FROM memory").Scan(&oldest, &newest)
	s.Oldest = time.Unix(oldest, 0)
	s.Newest = time.Unix(newest, 0)

	// Build time
	var built string
	if err := conn.QueryRow("SELECT value FROM meta WHERE key = 'last_build'").Scan(&built); err == nil {
		s.Built, _ = time.Parse(time.RFC3339, built)
	}

	return s, nil
}

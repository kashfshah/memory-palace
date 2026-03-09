package store

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// EnsureVectorColumns adds the embedding and local_summary columns to the
// memory table if they don't already exist. Safe to call on every open.
func (db *DB) EnsureVectorColumns() error {
	for _, col := range []struct{ name, def string }{
		{"embedding", "BLOB"},
		{"local_summary", "TEXT"},
	} {
		_, err := db.conn.Exec(
			fmt.Sprintf("ALTER TABLE memory ADD COLUMN %s %s", col.name, col.def),
		)
		// SQLITE_ERROR (1) means column already exists — safe to ignore.
		if err != nil && !isSQLiteAlreadyExists(err) {
			return fmt.Errorf("add column %s: %w", col.name, err)
		}
	}
	return nil
}

// isSQLiteAlreadyExists returns true for "duplicate column name" errors.
func isSQLiteAlreadyExists(err error) bool {
	return err != nil && (containsStr(err.Error(), "duplicate column") ||
		containsStr(err.Error(), "already exists"))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// EmbeddingCandidate is a record that needs embedding.
type EmbeddingCandidate struct {
	ID    int64
	Title string
	Body  string
}

// GetUnembeddedRecords returns up to limit records that have no embedding yet.
func (db *DB) GetUnembeddedRecords(limit int) ([]EmbeddingCandidate, error) {
	rows, err := db.conn.Query(`
		SELECT id, COALESCE(title,''), COALESCE(body,'')
		FROM memory
		WHERE embedding IS NULL
		ORDER BY
			CASE source
				WHEN 'safari_bookmarks' THEN 0
				WHEN 'notes' THEN 1
				WHEN 'zotero' THEN 2
				ELSE 3
			END,
			timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EmbeddingCandidate
	for rows.Next() {
		var c EmbeddingCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.Body); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// SetEmbedding stores a float32 embedding vector for a record as raw LE bytes.
func (db *DB) SetEmbedding(id int64, vec []float32) error {
	blob := float32sToBytes(vec)
	_, err := db.conn.Exec("UPDATE memory SET embedding = ? WHERE id = ?", blob, id)
	return err
}

// SetLocalSummary stores a locally-generated summary for a record.
func (db *DB) SetLocalSummary(id int64, summary string) error {
	_, err := db.conn.Exec("UPDATE memory SET local_summary = ? WHERE id = ?", summary, id)
	return err
}

// EmbedStats returns counts of embedded vs total records.
func (db *DB) EmbedStats() (embedded, total int, err error) {
	err = db.conn.QueryRow(`
		SELECT
			COUNT(CASE WHEN embedding IS NOT NULL THEN 1 END),
			COUNT(*)
		FROM memory
	`).Scan(&embedded, &total)
	return
}

// StoredEmbedding holds an ID and its vector for similarity search.
type StoredEmbedding struct {
	ID  int64
	Vec []float32
}

// LoadEmbeddings loads all stored embeddings. For large indexes, callers
// should use this once and cache the result.
func (db *DB) LoadEmbeddings() ([]StoredEmbedding, error) {
	rows, err := db.conn.Query(
		"SELECT id, embedding FROM memory WHERE embedding IS NOT NULL",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StoredEmbedding
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		vec := bytesToFloat32s(blob)
		if len(vec) > 0 {
			out = append(out, StoredEmbedding{ID: id, Vec: vec})
		}
	}
	return out, nil
}

// RecordsByIDs fetches full records for a set of IDs, preserving order.
func (db *DB) RecordsByIDs(ids []int64) ([]Record, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build IN clause.
	placeholders := make([]byte, 0, len(ids)*2)
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	rows, err := db.conn.Query(
		"SELECT id, source, timestamp, COALESCE(title,''), COALESCE(url,''), "+
			"COALESCE(body,''), COALESCE(location,'') "+
			"FROM memory WHERE id IN ("+string(placeholders)+")",
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]Record)
	for rows.Next() {
		var r Record
		var id, ts int64
		if err := rows.Scan(&id, &r.Source, &ts, &r.Title, &r.URL, &r.Body, &r.Location); err != nil {
			continue
		}
		r.Timestamp = time.Unix(ts, 0)
		byID[id] = r
	}

	// Return in the requested order (caller sorted by similarity).
	out := make([]Record, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

// GetSummarizableCandidates returns records with body text but no local_summary.
type SummarizableCandidate struct {
	ID    int64
	Title string
	Body  string
}

func (db *DB) GetSummarizableCandidates(limit int) ([]SummarizableCandidate, error) {
	rows, err := db.conn.Query(`
		SELECT id, COALESCE(title,''), COALESCE(body,'')
		FROM memory
		WHERE (local_summary IS NULL OR local_summary = '')
		  AND length(COALESCE(body,'')) > 50
		ORDER BY
			CASE source
				WHEN 'notes' THEN 0
				WHEN 'zotero' THEN 1
				WHEN 'safari_bookmarks' THEN 2
				ELSE 3
			END,
			timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SummarizableCandidate
	for rows.Next() {
		var c SummarizableCandidate
		if err := rows.Scan(&c.ID, &c.Title, &c.Body); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// ── Encoding helpers ──────────────────────────────────────────────────────────

func float32sToBytes(vecs []float32) []byte {
	buf := make([]byte, len(vecs)*4)
	for i, v := range vecs {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func bytesToFloat32s(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}


package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenCreatesDirAndSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	path := filepath.Join(dir, "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}
}

func TestUpsertAndQuery(t *testing.T) {
	db := tempDB(t)
	now := time.Now().Truncate(time.Second)

	records := []Record{
		{Source: "test", Timestamp: now, Title: "Alpha", URL: "https://alpha.com", RawID: "a1"},
		{Source: "test", Timestamp: now.Add(-time.Hour), Title: "Beta", URL: "https://beta.com", RawID: "a2"},
	}

	n, err := db.Upsert("test", records)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 inserted, got %d", n)
	}

	// Rebuild FTS and query
	if err := db.RebuildFTS(); err != nil {
		t.Fatal(err)
	}

	// Upsert again replaces all records for that source
	records2 := []Record{
		{Source: "test", Timestamp: now, Title: "Gamma", URL: "https://gamma.com", RawID: "g1"},
	}
	n, err = db.Upsert("test", records2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 after re-upsert, got %d", n)
	}
}

func TestMultipleSources(t *testing.T) {
	db := tempDB(t)
	now := time.Now()

	db.Upsert("src_a", []Record{{Timestamp: now, Title: "A", RawID: "1"}})
	db.Upsert("src_b", []Record{{Timestamp: now, Title: "B1", RawID: "1"}, {Timestamp: now, Title: "B2", RawID: "2"}})

	// Re-upsert src_a should not affect src_b
	db.Upsert("src_a", []Record{{Timestamp: now, Title: "A-new", RawID: "1"}})

	var countB int
	db.conn.QueryRow("SELECT COUNT(*) FROM memory WHERE source = 'src_b'").Scan(&countB)
	if countB != 2 {
		t.Fatalf("expected 2 src_b records, got %d", countB)
	}
}

func TestSetMetaAndRead(t *testing.T) {
	db := tempDB(t)

	if err := db.SetMeta("version", "42"); err != nil {
		t.Fatal(err)
	}

	var val string
	err := db.conn.QueryRow("SELECT value FROM meta WHERE key = 'version'").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "42" {
		t.Fatalf("expected '42', got %q", val)
	}

	// Overwrite
	db.SetMeta("version", "43")
	db.conn.QueryRow("SELECT value FROM meta WHERE key = 'version'").Scan(&val)
	if val != "43" {
		t.Fatalf("expected '43', got %q", val)
	}
}

func TestEnrichWorkflow(t *testing.T) {
	db := tempDB(t)
	now := time.Now()

	records := []Record{
		{Timestamp: now, Title: "Page", URL: "https://example.com/page", RawID: "p1"},
		{Timestamp: now, Title: "No URL", RawID: "p2"},
		{Timestamp: now, Title: "Another", URL: "https://example.com/other", RawID: "p3"},
	}
	db.Upsert("test", records)

	candidates, err := db.GetUnenrichedURLs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 URL candidates, got %d", len(candidates))
	}

	// Enrich one
	db.SetSummary(candidates[0].ID, "A summary of the page")

	candidates2, _ := db.GetUnenrichedURLs(10)
	if len(candidates2) != 1 {
		t.Fatalf("expected 1 remaining candidate, got %d", len(candidates2))
	}

	enriched, total, _ := db.EnrichStats()
	if enriched != 1 || total != 2 {
		t.Fatalf("expected 1/2 enriched, got %d/%d", enriched, total)
	}
}

func TestFTSSearch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fts.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	records := []Record{
		{Timestamp: now, Title: "Quantum Computing Paper", URL: "https://arxiv.org/quantum", Body: "Entanglement and superposition", RawID: "q1"},
		{Timestamp: now, Title: "Bread Recipe", URL: "https://cooking.com/bread", Body: "Flour water yeast salt", RawID: "r1"},
	}
	db.Upsert("test", records)
	db.RebuildFTS()
	db.Close()

	results, err := Query(dbPath, "quantum")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'quantum', got %d", len(results))
	}
	if results[0].Title != "Quantum Computing Paper" {
		t.Fatalf("expected quantum paper, got %q", results[0].Title)
	}
}

func TestStatsFunction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.db")
	db, _ := Open(path)

	now := time.Now()
	earlier := now.Add(-24 * time.Hour)
	db.Upsert("src_a", []Record{{Timestamp: earlier, Title: "Old", RawID: "1"}})
	db.Upsert("src_b", []Record{{Timestamp: now, Title: "New", RawID: "1"}})
	db.SetMeta("last_build", time.Now().UTC().Format(time.RFC3339))
	db.Close()

	stats, err := Stats(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Fatalf("expected 2 total, got %d", stats.Total)
	}
	if stats.BySrc["src_a"] != 1 || stats.BySrc["src_b"] != 1 {
		t.Fatalf("unexpected source counts: %v", stats.BySrc)
	}
	if stats.Built.IsZero() {
		t.Fatal("expected non-zero build time")
	}
}

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/kashfshah/memory-palace/embedder"
	"github.com/kashfshah/memory-palace/enrichment"
	"github.com/kashfshah/memory-palace/extractors"
	"github.com/kashfshah/memory-palace/mcp"
	"github.com/kashfshah/memory-palace/store"
	"github.com/kashfshah/memory-palace/summarizer"
	"github.com/kashfshah/memory-palace/web"
)

func main() {
	dbPath := flag.String("db", "data/memory.db", "Path to the unified memory database")
	verbose := flag.Bool("v", false, "Verbose output")
	sourcesFlag := flag.String("sources", "all", "Comma-separated sources: safari_history,safari_bookmarks,calendar,reminders,notes,all")
	queryFlag := flag.String("query", "", "FTS query against the memory index")
	statsFlag := flag.Bool("stats", false, "Show index statistics")
	enrichFlag := flag.Bool("enrich", false, "Enrich unenriched URLs with Kagi Summarizer")
	enrichLimit := flag.Int("enrich-limit", 25, "Max URLs to enrich per run")
	kagiKey := flag.String("kagi-key", "", "Kagi API key (or set KAGI_API_KEY env)")
	serveFlag := flag.Bool("serve", false, "Start the web UI server")
	portFlag := flag.Int("port", 8484, "Web UI port (used with --serve)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (enables HTTPS)")
	tlsKey := flag.String("tls-key", "", "TLS key file (enables HTTPS)")
	authUser := flag.String("auth-user", "", "Basic auth username (required with --tls-cert)")
	authPass := flag.String("auth-pass", "", "Basic auth password (or MP_AUTH_PASS env)")
	migrateFlag := flag.Bool("migrate-to-zotero", false, "Export Safari bookmarks as Zotero RDF for import")
	dryRunFlag := flag.Bool("dry-run", false, "Show what would happen without saving")
	batchFlag := flag.Int("batch", 0, "Max items per batch (0 = all)")
	cleanupFlag := flag.Bool("zotero-cleanup", false, "Generate Zotero cleanup scripts (JS for Zotero console)")
	feedFlag := flag.Bool("feed", false, "Feed new Memory Palace URLs to ArchiveBox")
	feedBatch := flag.Int("feed-batch", 0, "Max URLs to feed per run (0 = no limit, submit all eligible)")
	sanitizeFlag  := flag.Bool("sanitize", false, "Immediately delete all blocked records from the DB (runs without re-extracting)")
	mcpFlag       := flag.Bool("mcp", false, "Start MCP stdio server (for Claude Desktop and other MCP clients)")
	embedFlag     := flag.Bool("embed", false, "Embed unembedded records using local NLEmbedding model")
	embedLimit    := flag.Int("embed-limit", 500, "Max records to embed per run")
	embedBin      := flag.String("embed-bin", "bin/mp-embed", "Path to mp-embed binary")
	summarizeFlag := flag.Bool("summarize-local", false, "Summarize records locally using FoundationModels")
	summarizeLimit := flag.Int("summarize-limit", 50, "Max records to summarize per run")
	summarizeBin  := flag.String("summarize-bin", "bin/mp-summarize", "Path to mp-summarize binary")
	flag.Parse()

	if *mcpFlag {
		srv := mcp.New(*dbPath, *embedBin)
		if err := srv.Run(); err != nil {
			log.Fatalf("mcp: %v", err)
		}
		return
	}

	if *embedFlag {
		if err := runEmbed(*dbPath, *embedBin, *embedLimit, *verbose); err != nil {
			log.Fatalf("embed: %v", err)
		}
		return
	}

	if *summarizeFlag {
		if err := runSummarizeLocal(*dbPath, *summarizeBin, *summarizeLimit, *verbose); err != nil {
			log.Fatalf("summarize-local: %v", err)
		}
		return
	}

	if *queryFlag != "" {
		results, err := store.Query(*dbPath, *queryFlag)
		if err != nil {
			log.Fatalf("query failed: %v", err)
		}
		for _, r := range results {
			fmt.Printf("[%s] %s — %s\n", r.Source, r.Timestamp.Format("2006-01-02 15:04"), r.Title)
			if r.URL != "" {
				fmt.Printf("       %s\n", r.URL)
			}
		}
		fmt.Printf("\n%d results\n", len(results))
		return
	}

	if *statsFlag {
		s, err := store.Stats(*dbPath)
		if err != nil {
			log.Fatalf("stats failed: %v", err)
		}
		fmt.Printf("Memory Palace — %s\n", *dbPath)
		fmt.Printf("Total records:  %d\n", s.Total)
		for src, count := range s.BySrc {
			fmt.Printf("  %-20s %d\n", src, count)
		}
		fmt.Printf("Oldest:         %s\n", s.Oldest.Format("2006-01-02"))
		fmt.Printf("Newest:         %s\n", s.Newest.Format("2006-01-02"))
		fmt.Printf("Built:          %s\n", s.Built.Format("2006-01-02 15:04:05"))
		return
	}

	if *serveFlag {
		var opts []web.Option
		if *tlsCert != "" && *tlsKey != "" {
			opts = append(opts, web.WithTLS(*tlsCert, *tlsKey))
		}
		pass := *authPass
		if pass == "" {
			pass = os.Getenv("MP_AUTH_PASS")
		}
		if *authUser != "" && pass != "" {
			opts = append(opts, web.WithBasicAuth(*authUser, pass))
		}
		srv := web.New(*dbPath, *portFlag, opts...)
		log.Fatal(srv.Start())
	}

	if *cleanupFlag {
		if err := generateCleanupScripts("data/zotero-cleanup"); err != nil {
			log.Fatalf("cleanup generation failed: %v", err)
		}
		return
	}

	if *feedFlag {
		if err := runArchiveBoxFeed(*dryRunFlag, *feedBatch); err != nil {
			log.Fatalf("archivebox feed failed: %v", err)
		}
		return
	}

	if *sanitizeFlag {
		db, err := store.Open(*dbPath)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		defer db.Close()
		n, err := db.DeleteBlocked(extractors.BlockedDomains, extractors.BlockedTitleSubstrings)
		if err != nil {
			log.Fatalf("sanitize failed: %v", err)
		}
		fmt.Printf("Deleted %d blocked records.\n", n)
		if n > 0 {
			if err := db.RebuildFTS(); err != nil {
				log.Printf("WARN: FTS rebuild failed: %v", err)
			}
		}
		return
	}

	if *migrateFlag {
		if *dryRunFlag {
			if err := runMigration(*dbPath, true, *verbose, *batchFlag); err != nil {
				log.Fatalf("migration failed: %v", err)
			}
		} else {
			if err := generateZoteroRDF(*dbPath, *batchFlag); err != nil {
				log.Fatalf("RDF export failed: %v", err)
			}
			if err := generateMigrationCSV(*dbPath, *batchFlag); err != nil {
				log.Printf("WARN: CSV export failed: %v", err)
			}
		}
		return
	}

	if *enrichFlag {
		key := *kagiKey
		if key == "" {
			key = os.Getenv("KAGI_API_KEY")
		}
		if key == "" {
			// Try loading from .dev.vars
			key = loadDevVar("KAGI_API_KEY")
		}
		if key == "" {
			log.Fatal("--kagi-key or KAGI_API_KEY required for enrichment")
		}

		db, err := store.Open(*dbPath)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		defer db.Close()

		candidates, err := db.GetUnenrichedURLs(*enrichLimit)
		if err != nil {
			log.Fatalf("get candidates: %v", err)
		}

		if len(candidates) == 0 {
			enriched, total, _ := db.EnrichStats()
			fmt.Printf("All URL records enriched (%d/%d)\n", enriched, total)
			return
		}

		fmt.Printf("Enriching %d URLs via Kagi Summarizer...\n", len(candidates))
		summarizer := enrichment.NewKagiSummarizer(key)
		success, skipped := 0, 0

		for i, c := range candidates {
			if *verbose {
				log.Printf("[%d/%d] %s", i+1, len(candidates), c.URL)
			}
			summary, err := summarizer.Summarize(c.URL)
			if err != nil {
				if errors.Is(err, enrichment.ErrBalanceLow) {
					log.Printf("STOP: %v", err)
					break
				}
				log.Printf("  WARN: %v", err)
				skipped++
				continue
			}
			if summary == "" {
				skipped++
				continue
			}
			if err := db.SetSummary(c.ID, summary); err != nil {
				log.Printf("  WARN: save failed: %v", err)
				skipped++
				continue
			}
			success++
			if *verbose && summarizer.Balance != nil {
				log.Printf("  OK (%d chars, balance: $%.2f)", len(summary), *summarizer.Balance)
			}
		}

		// Rebuild FTS to include new summaries
		if success > 0 {
			if err := db.RebuildFTS(); err != nil {
				log.Printf("WARN: FTS rebuild failed: %v", err)
			}
		}

		enriched, total, _ := db.EnrichStats()
		fmt.Printf("Enriched %d, skipped %d. Total: %d/%d URLs have summaries.\n",
			success, skipped, enriched, total)
		return
	}

	// Build / refresh the index
	start := time.Now()
	sources := extractors.ParseSources(*sourcesFlag)

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Per-source status written to indexer-status.json after the run.
	type sourceStatus struct {
		OK        bool      `json:"ok"`
		LastRun   time.Time `json:"last_run"`
		Total     int       `json:"total"`
		LastAdded int       `json:"last_added"`
		Error     string    `json:"error,omitempty"`
	}
	statusMap := make(map[string]sourceStatus)
	now := time.Now()

	total := 0
	for _, src := range sources {
		ext, ok := extractors.Registry[src]
		if !ok {
			log.Printf("unknown source: %s, skipping", src)
			continue
		}
		if *verbose {
			log.Printf("extracting: %s", src)
		}
		records, err := ext.Extract()
		if errors.Is(err, extractors.ErrNotConfigured) {
			statusMap[src] = sourceStatus{OK: false, LastRun: now, Error: "not configured"}
			if *verbose {
				log.Printf("  %s: not configured, skipping", src)
			}
			continue
		}
		if err != nil {
			log.Printf("WARN: %s extraction failed: %v", src, err)
			statusMap[src] = sourceStatus{OK: false, LastRun: now, Error: err.Error()}
			continue
		}
		before := len(records)
		records = extractors.SanitizeRecords(records)
		if *verbose && len(records) < before {
			log.Printf("  %s: sanitized %d → %d records", src, before, len(records))
		}
		prevCount := db.CountBySource(src)
		n, err := db.Upsert(src, records)
		if err != nil {
			log.Printf("WARN: %s upsert failed: %v", src, err)
			statusMap[src] = sourceStatus{OK: false, LastRun: now, Error: err.Error()}
			continue
		}
		delta := n - prevCount
		if *verbose {
			log.Printf("  %s: %d records (+%d)", src, n, delta)
		}
		statusMap[src] = sourceStatus{OK: true, LastRun: now, Total: n, LastAdded: delta}
		total += n
	}

	// Always run blocked-content purge after every index build.
	// Catches any records that slipped through source-level sanitization
	// and ensures the expanded blocklist takes effect immediately on each run.
	if deleted, err := db.DeleteBlocked(extractors.BlockedDomains, extractors.BlockedTitleSubstrings); err != nil {
		log.Printf("WARN: post-index sanitize failed: %v", err)
	} else if deleted > 0 {
		log.Printf("Sanitizer: deleted %d blocked records", deleted)
		total -= deleted
	}

	if err := db.RebuildFTS(); err != nil {
		log.Printf("WARN: FTS rebuild failed: %v", err)
	}

	elapsed := time.Since(start)
	fmt.Printf("Indexed %d records from %d sources in %s\n", total, len(sources), elapsed.Round(time.Millisecond))

	// Write per-source status for the health endpoint.
	// Merge with existing file so partial runs (--sources x,y) don't wipe
	// status entries for sources that weren't included in this run.
	statusPath := filepath.Join(filepath.Dir(*dbPath), "indexer-status.json")
	merged := make(map[string]sourceStatus)
	if existing, err := os.ReadFile(statusPath); err == nil {
		if err := json.Unmarshal(existing, &merged); err != nil {
			log.Printf("WARN: could not parse existing indexer-status.json (starting fresh): %v", err)
		}
	}
	for k, v := range statusMap {
		merged[k] = v
	}
	if data, err := json.Marshal(merged); err == nil {
		if err := os.WriteFile(statusPath, data, 0644); err != nil {
			log.Printf("WARN: could not write indexer-status.json: %v", err)
		}
	}

	// Append a history line for sparkline trendlines in the health UI.
	// Format: {"ts":<unix>, "<source>":<added>, ...}
	histPath := filepath.Join(filepath.Dir(*dbPath), "indexer-history.jsonl")
	histEntry := map[string]any{"ts": now.Unix()}
	for src, s := range statusMap {
		histEntry[src] = s.LastAdded
	}
	if line, err := json.Marshal(histEntry); err == nil {
		if f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			if _, err := f.Write(append(line, '\n')); err != nil {
				log.Printf("WARN: could not append to indexer-history.jsonl: %v", err)
			}
			f.Close()
		} else {
			log.Printf("WARN: could not open indexer-history.jsonl: %v", err)
		}
	}

	// Write build metadata
	if err := db.SetMeta("last_build", time.Now().UTC().Format(time.RFC3339)); err != nil {
		log.Printf("WARN: could not write build metadata: %v", err)
	}

	if _, err := os.Stdout.Write([]byte("")); err != nil {
		// stdout closed, fine
	}
}

// loadDevVar reads a key from .dev.vars in the working directory or parent.
func loadDevVar(key string) string {
	for _, path := range []string{".dev.vars", "../.dev.vars"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range splitLines(string(data)) {
			if len(line) == 0 || line[0] == '#' {
				continue
			}
			idx := indexOf(line, '=')
			if idx > 0 && line[:idx] == key {
				return line[idx+1:]
			}
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := range s {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func indexOf(s string, c byte) int {
	for i := range s {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func runEmbed(dbPath, binPath string, limit int, verbose bool) error {
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := db.EnsureVectorColumns(); err != nil {
		return fmt.Errorf("ensure columns: %w", err)
	}

	candidates, err := db.GetUnembeddedRecords(limit)
	if err != nil {
		return fmt.Errorf("get candidates: %w", err)
	}
	if len(candidates) == 0 {
		embedded, total, _ := db.EmbedStats()
		fmt.Printf("All records embedded (%d/%d)\n", embedded, total)
		return nil
	}
	fmt.Printf("Embedding %d records via NLEmbedding...\n", len(candidates))

	emb, err := embedder.New(binPath)
	if err != nil {
		return fmt.Errorf("start embedder: %w", err)
	}
	defer emb.Close()

	success, skipped := 0, 0
	for i, c := range candidates {
		text := summarizer.TextForEmbedding(c.Title, c.Body)
		if text == "" {
			skipped++
			continue
		}
		vec, err := emb.Embed(text)
		if err != nil {
			if verbose {
				log.Printf("  WARN [%d] %s: %v", c.ID, c.Title, err)
			}
			skipped++
			continue
		}
		if err := db.SetEmbedding(c.ID, vec); err != nil {
			log.Printf("  WARN: save embedding %d: %v", c.ID, err)
			skipped++
			continue
		}
		success++
		if verbose && i%50 == 0 {
			log.Printf("  [%d/%d] embedded", i+1, len(candidates))
		}
	}

	embedded, total, _ := db.EmbedStats()
	fmt.Printf("Embedded %d, skipped %d. Total: %d/%d records have embeddings.\n",
		success, skipped, embedded, total)
	return nil
}

func runSummarizeLocal(dbPath, binPath string, limit int, verbose bool) error {
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := db.EnsureVectorColumns(); err != nil {
		return fmt.Errorf("ensure columns: %w", err)
	}

	candidates, err := db.GetSummarizableCandidates(limit)
	if err != nil {
		return fmt.Errorf("get candidates: %w", err)
	}
	if len(candidates) == 0 {
		fmt.Println("No records with body text need local summarization.")
		return nil
	}
	fmt.Printf("Summarizing %d records via FoundationModels...\n", len(candidates))

	sum, err := summarizer.NewLocal(binPath)
	if err != nil {
		return fmt.Errorf("start summarizer: %w", err)
	}
	defer sum.Close()

	success, skipped := 0, 0
	for i, c := range candidates {
		text := summarizer.TextForEmbedding(c.Title, c.Body)
		s, err := sum.Summarize(text)
		if err != nil {
			if verbose {
				log.Printf("  WARN [%d] %s: %v", c.ID, c.Title, err)
			}
			skipped++
			continue
		}
		if err := db.SetLocalSummary(c.ID, s); err != nil {
			log.Printf("  WARN: save summary %d: %v", c.ID, err)
			skipped++
			continue
		}
		success++
		if verbose {
			log.Printf("  [%d/%d] %s", i+1, len(candidates), c.Title)
		}
	}

	fmt.Printf("Summarized %d, skipped %d.\n", success, skipped)
	return nil
}

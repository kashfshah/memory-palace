package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/kashfshah/memory-palace/enrichment"
)

// zoteroEnrichItem holds the minimal fields needed for enrichment.
type zoteroEnrichItem struct {
	ItemID int
	URL    string
	Title  string
}

// runZoteroEnrich queries Zotero for items with no abstractNote and a URL,
// fetches summaries via Kagi Summarizer, and writes them back to Zotero SQLite.
//
// Safety: requires Zotero to be closed. If Zotero is open (connector responds
// at :23119), this function returns an error rather than risk concurrent writes.
//
// limit: max items to enrich per run (0 = no limit). Use a small value (10-20)
// in automated launchd runs to control Kagi API spend.
func runZoteroEnrich(kagiKey string, limit int, dryRun bool) error {
	// Gate: refuse to write if Zotero is open.
	zc := newZoteroConnector()
	if err := zc.ping(); err == nil {
		return fmt.Errorf("zotero_enrich: Zotero is open (connector responded at :23119). " +
			"Close Zotero before running enrichment to avoid concurrent write conflicts.")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("zotero_enrich: home dir: %w", err)
	}
	dbPath := home + "/Zotero/zotero.sqlite"

	// Open Zotero SQLite directly for read-write (Zotero is confirmed closed above).
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("zotero_enrich: open: %w", err)
	}
	defer conn.Close()

	items, err := findUnenrichedItems(conn, limit)
	if err != nil {
		return fmt.Errorf("zotero_enrich: query: %w", err)
	}
	if len(items) == 0 {
		fmt.Println("zotero_enrich: no items found without abstractNote and with URL — nothing to do.")
		return nil
	}
	fmt.Printf("zotero_enrich: found %d items to enrich\n", len(items))

	// Resolve the abstractNote fieldID once.
	var abstractFieldID int
	if err := conn.QueryRow(`SELECT fieldID FROM fields WHERE fieldName = 'abstractNote'`).
		Scan(&abstractFieldID); err != nil {
		return fmt.Errorf("zotero_enrich: resolve abstractNote fieldID: %w", err)
	}

	kagi := enrichment.NewKagiSummarizer(kagiKey)
	kagi.MinBalance = 2.0 // stop at $2 remaining — Kagi spend guard

	enriched := 0
	skipped := 0
	for _, item := range items {
		log.Printf("  [%d/%d] Summarizing: %s", enriched+skipped+1, len(items), item.URL)

		if dryRun {
			log.Printf("    dry-run: would write abstract for itemID %d", item.ItemID)
			enriched++
			continue
		}

		summary, err := kagi.Summarize(item.URL)
		if err != nil {
			if err == enrichment.ErrBalanceLow {
				log.Printf("  STOP: Kagi balance below minimum — stopping enrichment.")
				break
			}
			log.Printf("    SKIP (summarize error): %v", err)
			skipped++
			continue
		}
		if summary == "" {
			log.Printf("    SKIP (empty summary)")
			skipped++
			continue
		}

		if err := writeZoteroAbstract(conn, item.ItemID, abstractFieldID, summary); err != nil {
			log.Printf("    SKIP (write error): %v", err)
			skipped++
			continue
		}

		if kagi.Balance != nil {
			log.Printf("    OK (%d chars, balance: $%.2f)", len(summary), *kagi.Balance)
		} else {
			log.Printf("    OK (%d chars)", len(summary))
		}
		enriched++
	}

	fmt.Printf("zotero_enrich: done — %d enriched, %d skipped\n", enriched, skipped)
	return nil
}

// findUnenrichedItems returns Zotero items with a URL but no abstractNote.
// Results are ordered by dateAdded DESC so recently added items get enriched first.
func findUnenrichedItems(conn *sql.DB, limit int) ([]zoteroEnrichItem, error) {
	query := `
		SELECT i.itemID,
		    MAX(CASE WHEN f.fieldName = 'url'          THEN idv.value END) AS url,
		    MAX(CASE WHEN f.fieldName = 'title'        THEN idv.value END) AS title
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN itemData id ON i.itemID = id.itemID
		LEFT JOIN fields f ON id.fieldID = f.fieldID
		LEFT JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE it.typeName NOT IN ('attachment', 'note')
		  AND i.itemID NOT IN (
		      SELECT id2.itemID FROM itemData id2
		      JOIN fields f2 ON id2.fieldID = f2.fieldID
		      WHERE f2.fieldName = 'abstractNote'
		  )
		GROUP BY i.itemID
		HAVING url IS NOT NULL
		ORDER BY i.dateAdded DESC`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []zoteroEnrichItem
	for rows.Next() {
		var it zoteroEnrichItem
		var title sql.NullString
		if err := rows.Scan(&it.ItemID, &it.URL, &title); err != nil {
			continue
		}
		it.Title = title.String
		items = append(items, it)
	}
	return items, rows.Err()
}

// writeZoteroAbstract upserts an abstractNote value for a Zotero item.
// Uses Zotero's EAV schema: itemDataValues → itemData.
func writeZoteroAbstract(conn *sql.DB, itemID, fieldID int, abstract string) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Insert the abstract value if not already present.
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO itemDataValues(value) VALUES (?)`, abstract,
	); err != nil {
		return fmt.Errorf("insert value: %w", err)
	}

	// Retrieve the valueID (may have existed before the insert).
	var valueID int
	if err := tx.QueryRow(
		`SELECT valueID FROM itemDataValues WHERE value = ?`, abstract,
	).Scan(&valueID); err != nil {
		return fmt.Errorf("get valueID: %w", err)
	}

	// Upsert the itemData row linking item → field → value.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO itemData(itemID, fieldID, valueID) VALUES (?, ?, ?)`,
		itemID, fieldID, valueID,
	); err != nil {
		return fmt.Errorf("upsert itemData: %w", err)
	}

	return tx.Commit()
}

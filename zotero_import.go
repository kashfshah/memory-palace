package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

// bookmark holds a Safari bookmark from the memory palace DB.
type bookmark struct {
	Title string
	URL   string
}

// domainCollectionMap maps URL domains/patterns to Zotero collection IDs.
var domainCollectionMap = map[string]string{
	// Dev (C2)
	"github.com": "C2", "gitlab.com": "C2", "stackoverflow.com": "C2",
	"developer.": "C2", "docs.": "C2", "pkg.go.dev": "C2",
	"crates.io": "C2", "npmjs.com": "C2", "pypi.org": "C2",
	"githubassets.com": "C2", "joshwcomeau.com": "C2",
	"microsoft.com": "C2", "learn.microsoft": "C2",
	// Computing (C16)
	"news.ycombinator": "C16", "lobste.rs": "C16", "hackaday.com": "C16",
	"arstechnica.com": "C16", "slashdot.org": "C16", "theregister.com": "C16",
	"wired.com": "C16", "hackaday.io": "C16", "medium.com": "C16",
	"miro.medium.com": "C16", "cacm.acm.org": "C16", "acm.org": "C16",
	"ieee.org": "C16", "bluemodus.com": "C16", "tildes.net": "C16",
	// Ops (C9)
	"digitalocean.com": "C9", "linode.com": "C9", "cloudflare.com": "C9",
	"aws.amazon.com": "C9",
	// Science (C15)
	"arxiv.org": "C15", "nature.com": "C15", "sciencedirect.com": "C15",
	"pnas.org": "C15", "scholar.google": "C15", "pubmed.ncbi": "C15",
	"researchgate.net": "C15", "quantamagazine.org": "C15",
	"science.org": "C15", "ncbi.nlm.nih.gov": "C15", "nautil.us": "C15",
	"stephenwolfram.com": "C15", "wikipedia.org": "C15",
	// Mathematics (C12)
	"mathworld.wolfram": "C12", "mathoverflow.net": "C12", "math.stackexchange": "C12",
	// Psychology (C11)
	"psychologytoday.com": "C11", "madinamerica.com": "C11",
	// News (C10)
	"nytimes.com": "C10", "bbc.com": "C10", "bbc.co.uk": "C10",
	"reuters.com": "C10", "apnews.com": "C10", "theguardian.com": "C10",
	"aljazeera.": "C10", "npr.org": "C10", "washingtonpost.com": "C10",
	"thetimes.co.uk": "C10", "thenation.com": "C10", "courierpress.com": "C10",
	"14news.com": "C10", "goodnewsnetwork.org": "C10", "i.guim.co.uk": "C10",
	// Politics (C23)
	"politico.com": "C23", "congress.gov": "C23", "whitehouse.gov": "C23",
	"in.gov": "C23", "ssa.gov": "C23", "gov.uk": "C23",
	// Religion (C14)
	"quran.com": "C14", "sunnah.com": "C14", "islam.stackexchange": "C14",
	// Electronics (C7)
	"adafruit.com": "C7", "sparkfun.com": "C7", "digikey.com": "C7",
	"mouser.com": "C7",
	// Make (C13)
	"instructables.com": "C13",
	// 3D Printing (C1)
	"thingiverse.com": "C1", "printables.com": "C1", "prusa3d.com": "C1",
	"cdn.thingiverse.com": "C1",
	// Gaming (C4)
	"store.steampowered": "C4", "itch.io": "C4", "yare.io": "C4",
	// Audio/Visual (C5)
	"youtube.com": "C5", "vimeo.com": "C5", "soundcloud.com": "C5",
	"apple.com": "C5",
	// Law / Human Rights (C25)
	"un.org": "C25", "amnesty.org": "C25", "hrw.org": "C25", "aclu.org": "C25",
	"unhcr.org": "C25", "beautifulpublicdata.com": "C25",
	// SDF / Plan9
	"sdf.org": "C17", "plan9.io": "C24", "9p.io": "C24", "cat-v.org": "C24",
}

// classifyURL returns a Zotero collection ID based on URL domain matching.
func classifyURL(url string) string {
	lower := strings.ToLower(url)
	for pattern, collectionID := range domainCollectionMap {
		if strings.Contains(lower, pattern) {
			return collectionID
		}
	}
	return ""
}

// loadExistingZoteroURLs reads all URLs from the Zotero DB for deduplication.
func loadExistingZoteroURLs(zoteroDBPath string) (map[string]bool, error) {
	conn, err := sql.Open("sqlite", zoteroDBPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT idv.value FROM items i
		JOIN itemData id ON i.itemID = id.itemID
		JOIN fields f ON id.fieldID = f.fieldID
		JOIN itemDataValues idv ON id.valueID = idv.valueID
		WHERE f.fieldName = 'url'
			AND i.itemID NOT IN (SELECT itemID FROM deletedItems)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	urls := make(map[string]bool)
	for rows.Next() {
		var u string
		rows.Scan(&u)
		urls[normalizeURL(u)] = true
	}
	return urls, nil
}

// normalizeURL strips trailing slashes and lowercases for comparison.
func normalizeURL(u string) string {
	return strings.ToLower(strings.TrimRight(u, "/"))
}

// loadSafariBookmarks reads safari_bookmarks from the memory palace DB.
func loadSafariBookmarks(dbPath string) ([]bookmark, error) {
	conn, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT COALESCE(title,''), COALESCE(url,'')
		FROM memory WHERE source = 'safari_bookmarks' AND url <> '' AND url LIKE 'http%'
		ORDER BY timestamp DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookmarks []bookmark
	for rows.Next() {
		var b bookmark
		rows.Scan(&b.Title, &b.URL)
		bookmarks = append(bookmarks, b)
	}
	return bookmarks, nil
}

// runMigration analyzes Safari → Zotero bookmark migration (dry-run only).
// Actual import happens via generated JS scripts for Zotero's console.
func runMigration(memoryDBPath string, dryRun bool, verbose bool, batchSize int) error {
	home, _ := os.UserHomeDir()
	zoteroDBPath := home + "/Zotero/zotero.sqlite"

	log.Println("Loading existing Zotero URLs for deduplication...")
	existing, err := loadExistingZoteroURLs(zoteroDBPath)
	if err != nil {
		return fmt.Errorf("load zotero urls: %w", err)
	}
	log.Printf("  Found %d existing URLs in Zotero", len(existing))

	log.Println("Loading Safari bookmarks from Memory Palace...")
	bookmarks, err := loadSafariBookmarks(memoryDBPath)
	if err != nil {
		return fmt.Errorf("load bookmarks: %w", err)
	}
	log.Printf("  Found %d Safari bookmarks", len(bookmarks))

	// Filter out duplicates
	var toImport []bookmark
	for _, b := range bookmarks {
		if !existing[normalizeURL(b.URL)] {
			toImport = append(toImport, b)
		}
	}
	log.Printf("  %d new bookmarks after deduplication (%d already in Zotero)",
		len(toImport), len(bookmarks)-len(toImport))

	if batchSize > 0 && len(toImport) > batchSize {
		log.Printf("  Limiting to batch of %d (use --batch 0 for all)", batchSize)
		toImport = toImport[:batchSize]
	}

	// Classify into collections
	collectionCounts := map[string]int{}
	for _, b := range toImport {
		col := classifyURL(b.URL)
		if col == "" {
			col = "(uncategorized)"
		}
		collectionCounts[col]++
	}
	log.Println("  Collection distribution:")
	for col, count := range collectionCounts {
		log.Printf("    %-20s %d", col, count)
	}

	if dryRun {
		log.Println("\nDRY RUN — no items saved. Use --zotero-cleanup to generate import scripts.")
		for i, b := range toImport {
			col := classifyURL(b.URL)
			if col == "" {
				col = "L1"
			}
			fmt.Printf("  [%d] %s → %s\n", i+1, b.URL, col)
			if i >= 19 {
				fmt.Printf("  ... and %d more\n", len(toImport)-20)
				break
			}
		}
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

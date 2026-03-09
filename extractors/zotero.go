package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// Zotero extracts items from a local Zotero SQLite database.
// Reads: items, itemData, itemDataValues, fields, itemTypes, creators,
// itemCreators, itemTags, tags, collections, collectionItems.
type Zotero struct{}

func (z *Zotero) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Zotero/zotero.sqlite"

	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("zotero db not found at %s", dbPath)
	}

	conn, err := sql.Open("sqlite", dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open zotero db: %w", err)
	}
	defer conn.Close()

	// Pull items with metadata fields via EAV join
	rows, err := conn.Query(`
		SELECT
			i.itemID,
			it.typeName,
			i.dateAdded,
			i.dateModified,
			GROUP_CONCAT(CASE WHEN f.fieldName='title' THEN idv.value END) as title,
			GROUP_CONCAT(CASE WHEN f.fieldName='url' THEN idv.value END) as url,
			GROUP_CONCAT(CASE WHEN f.fieldName='date' THEN idv.value END) as itemDate,
			GROUP_CONCAT(CASE WHEN f.fieldName='abstractNote' THEN idv.value END) as abstract,
			GROUP_CONCAT(CASE WHEN f.fieldName='publicationTitle' THEN idv.value END) as publication
		FROM items i
		JOIN itemTypes it ON i.itemTypeID = it.itemTypeID
		LEFT JOIN itemData id ON i.itemID = id.itemID
		LEFT JOIN itemDataValues idv ON id.valueID = idv.valueID
		LEFT JOIN fields f ON id.fieldID = f.fieldID
		WHERE i.itemID NOT IN (SELECT itemID FROM deletedItems)
			AND it.typeName NOT IN ('attachment', 'note')
		GROUP BY i.itemID
		ORDER BY i.dateAdded DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	// Build lookup maps for creators, tags, collections
	creatorMap := z.loadCreators(conn)
	tagMap := z.loadTags(conn)
	collectionMap := z.loadCollections(conn)

	var records []store.Record
	for rows.Next() {
		var (
			itemID                                  int64
			typeName, dateAdded, dateModified       string
			title, url, itemDate, abstract, pubTitle sql.NullString
		)
		if err := rows.Scan(&itemID, &typeName, &dateAdded, &dateModified,
			&title, &url, &itemDate, &abstract, &pubTitle); err != nil {
			continue
		}

		// Parse timestamp — prefer dateAdded
		ts := parseZoteroTime(dateAdded)

		// Build body from abstract + metadata
		var bodyParts []string
		if abstract.Valid && abstract.String != "" {
			bodyParts = append(bodyParts, abstract.String)
		}
		if authors, ok := creatorMap[itemID]; ok {
			bodyParts = append(bodyParts, "Authors: "+authors)
		}
		if pubTitle.Valid && pubTitle.String != "" {
			bodyParts = append(bodyParts, "Publication: "+pubTitle.String)
		}
		if tags, ok := tagMap[itemID]; ok {
			bodyParts = append(bodyParts, "Tags: "+tags)
		}

		// Collection as location
		location := ""
		if col, ok := collectionMap[itemID]; ok {
			location = col
		}

		titleStr := title.String
		if titleStr == "" {
			titleStr = "[" + typeName + "]"
		}

		records = append(records, store.Record{
			Source:    "zotero",
			Timestamp: ts,
			Title:    titleStr,
			URL:      url.String,
			Body:     strings.Join(bodyParts, "\n"),
			Location: location,
			RawID:    strconv.FormatInt(itemID, 10),
		})
	}

	return records, nil
}

func (z *Zotero) loadCreators(conn *sql.DB) map[int64]string {
	m := make(map[int64]string)
	rows, err := conn.Query(`
		SELECT ic.itemID, GROUP_CONCAT(
			CASE WHEN c.firstName != '' THEN c.firstName || ' ' || c.lastName
			     ELSE c.lastName END, '; ')
		FROM itemCreators ic
		JOIN creators c ON ic.creatorID = c.creatorID
		GROUP BY ic.itemID
	`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var authors string
		rows.Scan(&id, &authors)
		m[id] = authors
	}
	return m
}

func (z *Zotero) loadTags(conn *sql.DB) map[int64]string {
	m := make(map[int64]string)
	rows, err := conn.Query(`
		SELECT itg.itemID, GROUP_CONCAT(t.name, ', ')
		FROM itemTags itg
		JOIN tags t ON itg.tagID = t.tagID
		GROUP BY itg.itemID
	`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var tags string
		rows.Scan(&id, &tags)
		m[id] = tags
	}
	return m
}

func (z *Zotero) loadCollections(conn *sql.DB) map[int64]string {
	m := make(map[int64]string)
	rows, err := conn.Query(`
		SELECT ci.itemID, GROUP_CONCAT(c.collectionName, ', ')
		FROM collectionItems ci
		JOIN collections c ON ci.collectionID = c.collectionID
		GROUP BY ci.itemID
	`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var collections string
		rows.Scan(&id, &collections)
		m[id] = collections
	}
	return m
}

// parseZoteroTime parses Zotero's date format: "2026-01-22 11:46:43"
func parseZoteroTime(s string) time.Time {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now()
}

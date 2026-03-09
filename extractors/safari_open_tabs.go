package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// SafariOpenTabs extracts currently open (non-private) tabs from Safari's
// container database. Private tab groups are excluded by walking the folder
// hierarchy and filtering out any tab parented under a "Private" or
// "privatePinned" group.
type SafariOpenTabs struct{}

func (s *SafariOpenTabs) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Containers/com.apple.Safari/Data/Library/Safari/SafariTabs.db"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot safari tabs: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open safari tabs: %w", err)
	}
	defer conn.Close()

	// Select non-private, non-deleted tabs with URLs.
	// Private tabs live under folders titled "Private" or "privatePinned".
	// We use a recursive CTE to collect all descendants of those folders
	// and exclude them.
	rows, err := conn.Query(`
		WITH RECURSIVE private_tree AS (
			SELECT id FROM bookmarks
			WHERE title IN ('Private', 'privatePinned') AND type = 1
			UNION ALL
			SELECT b.id FROM bookmarks b
			JOIN private_tree pt ON b.parent = pt.id
		)
		SELECT b.id, b.url, COALESCE(b.title, ''), b.last_modified
		FROM bookmarks b
		WHERE b.url IS NOT NULL AND b.url != ''
			AND b.deleted = 0
			AND b.hidden = 0
			AND b.id NOT IN (SELECT id FROM private_tree)
		ORDER BY b.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query safari tabs: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id int64
		var url, title string
		var lastMod sql.NullFloat64
		if err := rows.Scan(&id, &url, &title, &lastMod); err != nil {
			continue
		}

		ts := time.Now()
		if lastMod.Valid && lastMod.Float64 > 0 {
			// Core Data epoch (2001-01-01)
			unixTime := int64(lastMod.Float64) + coreDataEpoch
			ts = time.Unix(unixTime, 0)
		}

		if title == "" {
			title = url
		}

		records = append(records, store.Record{
			Source:    "safari_open_tabs",
			Timestamp: ts,
			Title:     title,
			URL:       url,
			RawID:     "tab-" + strconv.FormatInt(id, 10),
		})
	}

	return records, nil
}

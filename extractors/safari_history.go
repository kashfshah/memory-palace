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

// Core Data epoch offset: 2001-01-01 00:00:00 UTC
const coreDataEpoch = 978307200

// SafariHistory extracts browsing history from Safari's History.db.
type SafariHistory struct{}

func (s *SafariHistory) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Safari/History.db"

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot safari history: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open safari history: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT
			hi.id,
			hi.url,
			COALESCE(hv.title, ''),
			hv.visit_time,
			hi.visit_count
		FROM history_items hi
		JOIN history_visits hv ON hv.history_item = hi.id
		WHERE hv.visit_time IS NOT NULL
		ORDER BY hv.visit_time DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query safari history: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id int64
		var url, title string
		var visitTime float64
		var visitCount int
		if err := rows.Scan(&id, &url, &title, &visitTime, &visitCount); err != nil {
			continue
		}

		unixTime := int64(visitTime) + coreDataEpoch
		ts := time.Unix(unixTime, 0)

		if title == "" {
			title = url
		}

		records = append(records, store.Record{
			Source:    "safari_history",
			Timestamp: ts,
			Title:    title,
			URL:      url,
			Body:     fmt.Sprintf("visits: %d", visitCount),
			RawID:    strconv.FormatInt(id, 10) + "-" + strconv.FormatInt(int64(visitTime), 10),
		})
	}

	return records, nil
}

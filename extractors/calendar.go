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

// Calendar extracts events from Apple Calendar's SQLite database.
type Calendar struct{}

func (c *Calendar) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Group Containers/group.com.apple.calendar/Calendar.sqlitedb"

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot calendar db: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open calendar db: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT
			ci.ROWID,
			COALESCE(ci.summary, ''),
			ci.start_date,
			COALESCE(ci.description, ''),
			COALESCE(l.title, ''),
			COALESCE(c.title, '')
		FROM CalendarItem ci
		LEFT JOIN Location l ON ci.location_id = l.ROWID
		LEFT JOIN Calendar c ON ci.calendar_id = c.ROWID
		WHERE ci.start_date IS NOT NULL
		ORDER BY ci.start_date DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query calendar: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id int64
		var summary string
		var startDate float64
		var desc, loc, calName string
		if err := rows.Scan(&id, &summary, &startDate, &desc, &loc, &calName); err != nil {
			continue
		}

		// Calendar uses Core Data epoch
		unixTime := int64(startDate) + coreDataEpoch
		ts := time.Unix(unixTime, 0)

		body := ""
		if desc != "" {
			body = desc
		}
		if calName != "" {
			if body != "" {
				body += " | "
			}
			body += "cal: " + calName
		}

		records = append(records, store.Record{
			Source:    "calendar",
			Timestamp: ts,
			Title:    summary,
			Body:     body,
			Location: loc,
			RawID:    strconv.FormatInt(id, 10),
		})
	}

	return records, nil
}

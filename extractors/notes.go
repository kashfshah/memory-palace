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

// Notes extracts note titles and metadata from Apple Notes' NoteStore.sqlite.
// macOS 26 moved titles to ZTITLE1 and dates to ZCREATIONDATE3/ZMODIFICATIONDATE1
// on the ICNote entity (Z_ENT=12). The old ZTITLE/ZCREATIONDATE columns are empty.
type Notes struct{}

func (n *Notes) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Group Containers/group.com.apple.notes/NoteStore.sqlite"

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot notes db: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open notes db: %w", err)
	}
	defer conn.Close()

	// Query ICNote entities (Z_ENT=12) directly.
	// ZTITLE1 holds the title (ZTITLE is empty on macOS 26).
	// ZCREATIONDATE3/ZMODIFICATIONDATE1 hold dates (ZCREATIONDATE/ZMODIFICATIONDATE are empty).
	// ZSNIPPET holds a text preview of the note body.
	rows, err := conn.Query(`
		SELECT
			n.Z_PK,
			COALESCE(n.ZTITLE1, ''),
			COALESCE(n.ZCREATIONDATE3, 0),
			COALESCE(n.ZMODIFICATIONDATE1, 0),
			COALESCE(n.ZSNIPPET, ''),
			COALESCE(folder.ZTITLE2, ''),
			COALESCE(n.ZIDENTIFIER, '')
		FROM ZICCLOUDSYNCINGOBJECT n
		LEFT JOIN ZICCLOUDSYNCINGOBJECT folder
			ON n.ZFOLDER = folder.Z_PK AND folder.Z_ENT = 15
		WHERE n.Z_ENT = 12
			AND n.ZTITLE1 IS NOT NULL
			AND n.ZTITLE1 != ''
			AND COALESCE(n.ZMARKEDFORDELETION, 0) != 1
		ORDER BY n.ZMODIFICATIONDATE1 DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query notes: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id int64
		var title string
		var createDate, modDate float64
		var snippet, folder, identifier string
		if err := rows.Scan(&id, &title, &createDate, &modDate, &snippet, &folder, &identifier); err != nil {
			continue
		}

		// Pick best available date: modification > creation > epoch zero
		bestDate := modDate
		if bestDate == 0 {
			bestDate = createDate
		}

		var ts time.Time
		if bestDate > 0 {
			ts = time.Unix(int64(bestDate)+coreDataEpoch, 0)
		} else {
			ts = time.Unix(0, 0)
		}

		body := snippet
		if folder != "" {
			if body != "" {
				body += " | "
			}
			body += "folder: " + folder
		}

		var noteURL string
		if identifier != "" {
			noteURL = "notes://showNote?identifier=" + identifier
		}

		records = append(records, store.Record{
			Source:    "notes",
			Timestamp: ts,
			Title:     title,
			URL:       noteURL,
			Body:      body,
			RawID:     strconv.FormatInt(id, 10),
		})
	}

	return records, nil
}

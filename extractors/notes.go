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
// Body content lives in gzipped protobuf (ZICNOTEDATA.ZDATA) — deferred to Phase 3.
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

	rows, err := conn.Query(`
		SELECT
			n.Z_PK,
			COALESCE(n.ZTITLE, ''),
			COALESCE(n.ZCREATIONDATE, 0),
			COALESCE(n.ZMODIFICATIONDATE, 0),
			COALESCE(n.ZSNIPPET, ''),
			COALESCE(folder.ZTITLE2, ''),
			COALESCE(n.ZIDENTIFIER, '')
		FROM ZICCLOUDSYNCINGOBJECT n
		LEFT JOIN ZICCLOUDSYNCINGOBJECT folder
			ON n.ZFOLDER = folder.Z_PK
		WHERE n.ZTITLE IS NOT NULL
			AND COALESCE(n.ZMARKEDFORDELETION, 0) != 1
		ORDER BY n.ZMODIFICATIONDATE DESC
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

		// Notes uses Core Data epoch
		unixTime := int64(modDate) + coreDataEpoch
		ts := time.Unix(unixTime, 0)

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

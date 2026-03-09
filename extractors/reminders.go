package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// Reminders extracts tasks from Apple Reminders' SQLite database.
type Reminders struct{}

func (r *Reminders) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	storesDir := home + "/Library/Group Containers/group.com.apple.reminders/Container_v1/Stores"

	// Find the Data-*.sqlite file
	matches, err := filepath.Glob(storesDir + "/Data-*.sqlite")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("no reminders database found in %s", storesDir)
	}

	snapPath, cleanup, err := snapshotDB(matches[0])
	if err != nil {
		return nil, fmt.Errorf("snapshot reminders db: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open reminders db: %w", err)
	}
	defer conn.Close()

	// Try the schema — Reminders uses different table names across versions
	rows, err := conn.Query(`
		SELECT name FROM sqlite_master WHERE type='table' AND name LIKE '%REMINDER%' OR name LIKE '%TASK%'
	`)
	if err != nil {
		return nil, fmt.Errorf("inspect reminders schema: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()

	// Try common Reminders table patterns
	queries := []string{
		`SELECT ROWID, COALESCE(ZTITLE,''), COALESCE(ZCREATIONDATE,0), COALESCE(ZNOTES,''), ZCOMPLETED FROM ZREMCDREMINDER ORDER BY ZCREATIONDATE DESC`,
		`SELECT ROWID, COALESCE(title,''), COALESCE(creation_date,0), COALESCE(notes,''), completed FROM reminders ORDER BY creation_date DESC`,
	}

	for _, q := range queries {
		records, err := tryRemindersQuery(conn, q)
		if err == nil {
			return records, nil
		}
	}

	return nil, fmt.Errorf("could not query reminders, tables found: %v", tables)
}

func tryRemindersQuery(conn *sql.DB, query string) ([]store.Record, error) {
	rows, err := conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id int64
		var title string
		var createDate float64
		var notes string
		var completed int
		if err := rows.Scan(&id, &title, &createDate, &notes, &completed); err != nil {
			continue
		}

		unixTime := int64(createDate) + coreDataEpoch
		ts := time.Unix(unixTime, 0)

		body := notes
		if completed != 0 {
			body = "[completed] " + body
		}

		records = append(records, store.Record{
			Source:    "reminders",
			Timestamp: ts,
			Title:    title,
			Body:     body,
			RawID:    strconv.FormatInt(id, 10),
		})
	}

	return records, nil
}

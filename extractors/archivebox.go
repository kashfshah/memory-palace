package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// ArchiveBox extracts archived web snapshots from an ArchiveBox SQLite database.
// The database lives on cabinet (192.168.0.8) and gets synced locally via rsync.
type ArchiveBox struct{}

const archiveBoxLocalDB = "/tmp/archivebox-index.sqlite3"
const archiveBoxRemotePath = "archivebox/home/archivebox/data/index.sqlite3"

func (a *ArchiveBox) Extract() ([]store.Record, error) {
	// Sync the DB from cabinet via incus file pull
	if err := syncArchiveBoxDB(); err != nil {
		return nil, fmt.Errorf("sync archivebox db: %w", err)
	}

	conn, err := sql.Open("sqlite", archiveBoxLocalDB+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open archivebox db: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT
			s.id,
			s.url,
			COALESCE(s.title, ''),
			s.added,
			s.updated,
			COALESCE(GROUP_CONCAT(t.name, ', '), '') as tags
		FROM core_snapshot s
		LEFT JOIN core_snapshot_tags st ON st.snapshot_id = s.id
		LEFT JOIN core_tag t ON t.id = st.tag_id
		GROUP BY s.id
		ORDER BY s.added DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query archivebox: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var id, url, title, tags string
		var added string
		var updated sql.NullString
		if err := rows.Scan(&id, &url, &title, &added, &updated, &tags); err != nil {
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, added)
		if err != nil {
			ts, err = time.Parse("2006-01-02 15:04:05.000000", added)
		}
		if err != nil {
			ts, err = time.Parse("2006-01-02 15:04:05", added)
			if err != nil {
				continue
			}
		}

		if title == "" {
			title = url
		}

		body := "archived"
		if tags != "" {
			body = "tags: " + tags
		}

		records = append(records, store.Record{
			Source:    "archivebox",
			Timestamp: ts,
			Title:     title,
			URL:       url,
			Body:      body,
			Location:  "cabinet",
			RawID:     id,
		})
	}

	return records, nil
}

// syncArchiveBoxDB pulls the ArchiveBox SQLite DB from cabinet to local machine.
func syncArchiveBoxDB() error {
	// Step 1: pull from incus container to cabinet host
	remoteTmp := "/tmp/archivebox-index.sqlite3"
	cmd := exec.Command("ssh", "cabinet",
		"incus", "file", "pull",
		archiveBoxRemotePath,
		remoteTmp,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("incus file pull: %w", err)
	}

	// Step 2: rsync from cabinet host to local machine
	cmd = exec.Command("rsync", "-az", "--timeout=30",
		"cabinet:"+remoteTmp,
		archiveBoxLocalDB,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

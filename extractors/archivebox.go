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
//
// Configuration via environment variables (all optional; omit to skip):
//
//	ARCHIVEBOX_DB             — direct local path to index.sqlite3 (highest priority)
//	ARCHIVEBOX_SSH_HOST       — SSH host to rsync the DB from (e.g. "cabinet")
//	ARCHIVEBOX_SSH_PATH       — path on SSH host (default: /tmp/archivebox-index.sqlite3)
//	ARCHIVEBOX_INCUS_CONTAINER — Incus container name to pull from before rsyncing
//	ARCHIVEBOX_INCUS_PATH     — path inside Incus container (e.g. "archivebox/home/archivebox/data/index.sqlite3")
//
// Typical local install:   ARCHIVEBOX_DB=/path/to/data/index.sqlite3
// Typical remote install:  ARCHIVEBOX_SSH_HOST=myserver ARCHIVEBOX_SSH_PATH=/home/archivebox/data/index.sqlite3
// kashif/cabinet setup:    ARCHIVEBOX_SSH_HOST=cabinet ARCHIVEBOX_INCUS_CONTAINER=archivebox
//                          ARCHIVEBOX_INCUS_PATH=archivebox/home/archivebox/data/index.sqlite3
type ArchiveBox struct{}

const archiveBoxLocalTmp = "/tmp/mp-archivebox-index.sqlite3"

func (a *ArchiveBox) Extract() ([]store.Record, error) {
	dbPath, err := resolveArchiveBoxDB()
	if err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath+"?mode=ro")
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
		if err := rows.Scan(&id, &url, &title, &added, &tags); err != nil {
			continue
		}

		ts, err := parseArchiveBoxTime(added)
		if err != nil {
			continue
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
			RawID:     id,
		})
	}

	return records, nil
}

// resolveArchiveBoxDB returns a local readable path to the ArchiveBox SQLite DB,
// syncing from remote if needed. Returns ErrNotConfigured if no source is set.
func resolveArchiveBoxDB() (string, error) {
	// 1. Direct local path — snapshot to avoid lock conflicts.
	if dbPath := os.Getenv("ARCHIVEBOX_DB"); dbPath != "" {
		dst, cleanup, err := snapshotDB(dbPath)
		if err != nil {
			return "", fmt.Errorf("snapshot archivebox db: %w", err)
		}
		_ = cleanup // caller closes connection; tmp file is overwritten next run
		return dst, nil
	}

	// 2. SSH (optionally via Incus container).
	sshHost := os.Getenv("ARCHIVEBOX_SSH_HOST")
	if sshHost == "" {
		return "", ErrNotConfigured
	}

	sshPath := os.Getenv("ARCHIVEBOX_SSH_PATH")
	if sshPath == "" {
		sshPath = "/tmp/mp-archivebox-index.sqlite3"
	}

	// If Incus container is specified, pull from it to the SSH host first.
	incusContainer := os.Getenv("ARCHIVEBOX_INCUS_CONTAINER")
	if incusContainer != "" {
		incusPath := os.Getenv("ARCHIVEBOX_INCUS_PATH")
		if incusPath == "" {
			return "", fmt.Errorf("ARCHIVEBOX_INCUS_PATH required when ARCHIVEBOX_INCUS_CONTAINER is set")
		}
		cmd := exec.Command("ssh", sshHost,
			"incus", "file", "pull", incusPath, sshPath,
		)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("incus file pull: %w", err)
		}
	}

	// Rsync from SSH host to local tmp.
	cmd := exec.Command("rsync", "-az", "--timeout=30",
		sshHost+":"+sshPath,
		archiveBoxLocalTmp,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rsync from %s: %w", sshHost, err)
	}

	return archiveBoxLocalTmp, nil
}

func parseArchiveBoxTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.000000",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time format: %q", s)
}

// Ensure ArchiveBox implements Extractor.
var _ Extractor = (*ArchiveBox)(nil)

// Compile-time check that sql is used.
var _ = (*sql.DB)(nil)

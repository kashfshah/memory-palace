package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kashfshah/memory-palace/store"
)

const coreDateEpochOffset = 978307200 // seconds between Unix epoch and Core Data epoch (2001-01-01)

// Contacts extracts Apple Contacts.app entries from AddressBook-v22.abcddb.
// Indexes one record per contact with full name, organisation, and all
// email addresses in the body for use as a lookup table when enriching
// other sources (e.g. Protonmail sender resolution).
type Contacts struct{}

func (c *Contacts) Extract() ([]store.Record, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, ErrNotConfigured
	}

	// The main AddressBook-v22.abcddb has only the "me" card (3 rows).
	// Real contacts live in the first Sources/<uuid>/AddressBook-v22.abcddb.
	dbPath, err := findContactsDB(home)
	if err != nil {
		return nil, ErrNotConfigured
	}

	snap, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("contacts: snapshot: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snap+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("contacts: open: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT
			r.ZUNIQUEID,
			COALESCE(r.ZFIRSTNAME,''),
			COALESCE(r.ZLASTNAME,''),
			COALESCE(r.ZORGANIZATION,''),
			COALESCE(r.ZNICKNAME,''),
			COALESCE(r.ZMODIFICATIONDATE, r.ZCREATIONDATE, 0),
			GROUP_CONCAT(e.ZADDRESS, '|')
		FROM ZABCDRECORD r
		LEFT JOIN ZABCDEMAILADDRESS e ON e.ZOWNER = r.Z_PK
		WHERE (r.ZFIRSTNAME IS NOT NULL OR r.ZLASTNAME IS NOT NULL OR r.ZORGANIZATION IS NOT NULL)
		  AND r.ZUNIQUEID LIKE '%:ABPerson'
		GROUP BY r.Z_PK
		ORDER BY r.ZMODIFICATIONDATE DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("contacts: query: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var uniqueID, first, last, org, nick string
		var coreDate float64
		var emailsRaw sql.NullString

		if err := rows.Scan(&uniqueID, &first, &last, &org, &nick, &coreDate, &emailsRaw); err != nil {
			continue
		}

		fullName := strings.TrimSpace(first + " " + last)
		if fullName == "" {
			fullName = org
		}
		if fullName == "" {
			continue
		}

		// Build title: "First Last (Organisation)" or just the name.
		title := fullName
		if org != "" && org != fullName {
			title = fullName + " (" + org + ")"
		}
		if nick != "" {
			title += " [" + nick + "]"
		}

		// Body: all email addresses, one per line.
		var body string
		if emailsRaw.Valid && emailsRaw.String != "" {
			emails := strings.Split(emailsRaw.String, "|")
			body = strings.Join(emails, "\n")
		}

		ts := time.Unix(int64(coreDate)+coreDateEpochOffset, 0)
		if coreDate == 0 {
			ts = time.Time{}
		}

		// Strip the ":ABPerson" suffix from the unique ID for a clean raw_id.
		rawID := strings.TrimSuffix(uniqueID, ":ABPerson")

		records = append(records, store.Record{
			Source:    "contacts",
			Timestamp: ts,
			Title:     title,
			Body:      body,
			RawID:     rawID,
		})
	}
	return records, nil
}

// findContactsDB returns the path to the real contacts database in the
// first Sources/<uuid>/ subdirectory, falling back to the root DB.
func findContactsDB(home string) (string, error) {
	sourcesDir := home + "/Library/Application Support/AddressBook/Sources"
	entries, err := os.ReadDir(sourcesDir)
	if err != nil {
		// Try root-level DB as fallback.
		root := home + "/Library/Application Support/AddressBook/AddressBook-v22.abcddb"
		if _, err2 := os.Stat(root); err2 == nil {
			return root, nil
		}
		return "", ErrNotConfigured
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := sourcesDir + "/" + e.Name() + "/AddressBook-v22.abcddb"
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", ErrNotConfigured
}

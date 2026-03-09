package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// SafariICloudTabs extracts iCloud-synced tabs from all devices.
// These come from Safari's container CloudTabs.db which tracks open tabs
// across iPhone, iPad, and other Macs. Private tabs never sync to iCloud.
type SafariICloudTabs struct{}

func (s *SafariICloudTabs) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Containers/com.apple.Safari/Data/Library/Safari/CloudTabs.db"

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot cloud tabs: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open cloud tabs: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT t.tab_uuid, t.url, COALESCE(t.title, ''),
			d.device_name, t.last_viewed_time
		FROM cloud_tabs t
		JOIN cloud_tab_devices d ON t.device_uuid = d.device_uuid
		WHERE t.url IS NOT NULL AND t.url != ''
		ORDER BY t.last_viewed_time DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query cloud tabs: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var tabUUID, url, title, deviceName string
		var lastViewed float64
		if err := rows.Scan(&tabUUID, &url, &title, &deviceName, &lastViewed); err != nil {
			continue
		}

		ts := time.Now()
		if lastViewed > 0 {
			// Core Data epoch (2001-01-01)
			unixTime := int64(lastViewed) + coreDataEpoch
			ts = time.Unix(unixTime, 0)
		}

		if title == "" {
			title = url
		}

		body := "device: " + deviceName

		records = append(records, store.Record{
			Source:    "safari_icloud_tabs",
			Timestamp: ts,
			Title:     title,
			URL:       url,
			Body:      body,
			RawID:     "cloud-" + tabUUID,
		})
	}

	return records, nil
}

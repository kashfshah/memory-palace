package extractors

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kashfshah/memory-palace/store"
	_ "modernc.org/sqlite"
)

// KnowledgeC extracts app foreground-usage sessions from macOS knowledgeC.db.
// This is the same data source Screen Time uses — every app focus event with
// its start time and duration.
type KnowledgeC struct{}

// bundleNames maps common macOS bundle IDs to human-readable names.
var bundleNames = map[string]string{
	"com.apple.Safari":                    "Safari",
	"com.apple.Notes":                     "Notes",
	"com.apple.mail":                      "Mail",
	"com.apple.Messages":                  "Messages",
	"com.apple.Music":                     "Music",
	"com.apple.Podcasts":                  "Podcasts",
	"com.apple.Photos":                    "Photos",
	"com.apple.Maps":                      "Maps",
	"com.apple.reminders":                 "Reminders",
	"com.apple.iCal":                      "Calendar",
	"com.apple.systempreferences":         "System Preferences",
	"com.apple.SystemPreferences":         "System Settings",
	"com.apple.finder":                    "Finder",
	"com.apple.Terminal":                  "Terminal",
	"com.apple.ActivityMonitor":           "Activity Monitor",
	"com.apple.Preview":                   "Preview",
	"com.apple.TextEdit":                  "TextEdit",
	"com.apple.dt.Xcode":                  "Xcode",
	"com.googlecode.iterm2":               "iTerm2",
	"com.microsoft.VSCode":                "VS Code",
	"com.jetbrains.goland":                "GoLand",
	"com.jetbrains.pycharm":               "PyCharm",
	"com.jetbrains.intellij":              "IntelliJ",
	"com.tinyspeck.slackmacgap":           "Slack",
	"com.hnc.Discord":                     "Discord",
	"com.signal.Signal":                   "Signal",
	"org.whispersystems.signal-desktop":   "Signal",
	"com.notion.id":                       "Notion",
	"com.obsidian.md":                     "Obsidian",
	"com.agilebits.onepassword-osx":       "1Password",
	"com.figma.Desktop":                   "Figma",
	"com.spotify.client":                  "Spotify",
	"com.zoom.xos":                        "Zoom",
	"us.zoom.xos":                         "Zoom",
	"com.brave.Browser":                   "Brave",
	"com.google.Chrome":                   "Chrome",
	"org.mozilla.firefox":                 "Firefox",
	"com.apple.MobileSMS":                 "Messages",
	"com.apple.FaceTime":                  "FaceTime",
	"com.readdle.PDFExpert":               "PDF Expert",
}

// bundleToName converts a bundle ID to a readable app name.
// Falls back to stripping the vendor prefix and title-casing the app segment.
func bundleToName(bundle string) string {
	if name, ok := bundleNames[bundle]; ok {
		return name
	}
	// Strip common prefixes: com.apple.*, com.google.*, net.*, org.*
	parts := strings.Split(bundle, ".")
	if len(parts) >= 3 {
		// Last segment is usually the app name
		name := parts[len(parts)-1]
		if len(name) > 1 {
			return strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}
	return bundle
}

// formatDuration renders seconds as a human-readable string.
func formatDuration(secs int64) string {
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		return fmt.Sprintf("%dm %ds", secs/60, secs%60)
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	return fmt.Sprintf("%dh %dm", h, m)
}

func (k *KnowledgeC) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	dbPath := home + "/Library/Application Support/Knowledge/knowledgeC.db"

	snapPath, cleanup, err := snapshotDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("snapshot knowledgec db: %w", err)
	}
	defer cleanup()

	conn, err := sql.Open("sqlite", snapPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open knowledgec db: %w", err)
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT
			Z_PK,
			COALESCE(ZVALUESTRING, ''),
			COALESCE(ZSTARTDATE, 0),
			COALESCE(ZENDDATE, 0)
		FROM ZOBJECT
		WHERE ZSTREAMNAME = '/app/usage'
			AND ZVALUESTRING IS NOT NULL
			AND ZENDDATE > ZSTARTDATE
		ORDER BY ZSTARTDATE DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query knowledgec: %w", err)
	}
	defer rows.Close()

	var records []store.Record
	for rows.Next() {
		var pk int64
		var bundle string
		var startDate, endDate float64
		if err := rows.Scan(&pk, &bundle, &startDate, &endDate); err != nil {
			continue
		}

		durSecs := int64(endDate - startDate)
		// Skip noise — anything under 10 seconds is an accidental focus switch
		if durSecs < 10 {
			continue
		}

		startUnix := int64(startDate) + coreDataEpoch
		ts := time.Unix(startUnix, 0)
		appName := bundleToName(bundle)

		records = append(records, store.Record{
			Source:    "knowledgec",
			Timestamp: ts,
			Title:     appName + " — " + formatDuration(durSecs),
			Body:      bundle + " | " + formatDuration(durSecs) + " starting " + ts.Format("15:04"),
			RawID:     strconv.FormatInt(pk, 10),
		})
	}

	return records, nil
}

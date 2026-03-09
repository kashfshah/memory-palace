package extractors

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kashfshah/memory-palace/store"
)

// SafariBookmarks extracts bookmarks from Safari's Bookmarks.plist (binary plist).
// Uses plutil to convert to xml, then parses manually.
type SafariBookmarks struct{}

func (s *SafariBookmarks) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	plistPath := home + "/Library/Safari/Bookmarks.plist"

	// Convert binary plist to XML via plutil
	out, err := exec.Command("plutil", "-convert", "xml1", "-o", "-", plistPath).Output()
	if err != nil {
		return nil, fmt.Errorf("plutil convert: %w", err)
	}

	// Simple extraction: find all URLString values paired with preceding title
	// This avoids a full plist parser dependency
	lines := strings.Split(string(out), "\n")
	var records []store.Record
	var lastTitle string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.Contains(trimmed, "<key>URIDictionary</key>") ||
			strings.Contains(trimmed, "<key>title</key>") {
			// Next string value might be the title
			continue
		}

		if strings.HasPrefix(trimmed, "<string>") && strings.HasSuffix(trimmed, "</string>") {
			val := strings.TrimPrefix(trimmed, "<string>")
			val = strings.TrimSuffix(val, "</string>")

			if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
				records = append(records, store.Record{
					Source:    "safari_bookmarks",
					Timestamp: time.Now(), // plist doesn't store bookmark creation time
					Title:    lastTitle,
					URL:      val,
					RawID:    val,
				})
				lastTitle = ""
			} else if val != "" && !strings.HasPrefix(val, "<?xml") {
				lastTitle = val
			}
		}
	}

	return records, nil
}

// Verify plutil exists (macOS built-in).
func init() {
	if _, err := exec.LookPath("plutil"); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: plutil not found, safari_bookmarks extraction unavailable\n")
	}
}

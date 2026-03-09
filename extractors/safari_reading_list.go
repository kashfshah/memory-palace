package extractors

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/kashfshah/memory-palace/store"
)

// SafariReadingList extracts reading list entries from Safari's Bookmarks.plist.
// Uses Python's plistlib for binary plist parsing (plutil can't produce JSON
// from plists containing date/data types).
type SafariReadingList struct{}

type readingListItem struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	DateAdded string `json:"dateAdded"`
}

func (s *SafariReadingList) Extract() ([]store.Record, error) {
	home, _ := os.UserHomeDir()
	plistPath := home + "/Library/Safari/Bookmarks.plist"

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return nil, nil
	}

	// Python snippet: parse binary plist, find ReadingList folder, emit JSON
	script := fmt.Sprintf(`
import plistlib, json, os, sys

with open(%q, "rb") as f:
    safari = plistlib.load(f)

def find_rl(node):
    if node.get("Title") == "com.apple.ReadingList":
        return node
    for child in node.get("Children", []):
        result = find_rl(child)
        if result:
            return result
    return None

rl = find_rl(safari)
if not rl:
    print("[]")
    sys.exit(0)

items = []
for child in rl.get("Children", []):
    url = child.get("URLString", "")
    title = child.get("URIDictionary", {}).get("title", "")
    da = child.get("ReadingList", {}).get("DateAdded")
    if url:
        items.append({
            "url": url,
            "title": title,
            "dateAdded": da.isoformat() if da else "",
        })
print(json.dumps(items))
`, plistPath)

	out, err := exec.Command("python3", "-c", script).Output()
	if err != nil {
		return nil, fmt.Errorf("python3 plist parse: %w", err)
	}

	var items []readingListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	records := make([]store.Record, 0, len(items))
	for _, item := range items {
		ts := time.Now()
		if item.DateAdded != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, item.DateAdded); err == nil {
				ts = parsed
			} else if parsed, err := time.Parse("2006-01-02T15:04:05.999999", item.DateAdded); err == nil {
				ts = parsed
			} else if parsed, err := time.Parse("2006-01-02T15:04:05", item.DateAdded); err == nil {
				ts = parsed
			}
		}

		records = append(records, store.Record{
			Source:    "safari_reading_list",
			Timestamp: ts,
			Title:     item.Title,
			URL:       item.URL,
			RawID:     item.URL,
		})
	}

	return records, nil
}

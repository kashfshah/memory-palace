package extractors

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"github.com/kashfshah/memory-palace/store"
)

// Clipboard reads entries written by scripts/clipboard-monitor.py.
//
// Configure the JSONL path via CLIPBOARD_JSONL_PATH env var.
// Defaults to data/clipboard.jsonl relative to the working directory.
// Returns ErrNotConfigured when the file does not yet exist.
type Clipboard struct{}

type clipEntry struct {
	TS      int64  `json:"ts"`
	Machine string `json:"machine"`
	Content string `json:"content"`
	URL     string `json:"url"`
	Hash    string `json:"hash"`
}

func (c *Clipboard) Extract() ([]store.Record, error) {
	path := os.Getenv("CLIPBOARD_JSONL_PATH")
	if path == "" {
		path = "data/clipboard.jsonl"
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []store.Record
	scanner := bufio.NewScanner(f)
	// Clipboard entries can be large; raise the buffer limit.
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e clipEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if e.Hash == "" || e.Content == "" {
			continue
		}

		title := e.Content
		if len(title) > 120 {
			title = title[:120] + "…"
		}
		// Include machine name in title when the file aggregates multiple machines.
		if e.Machine != "" {
			title = "[" + e.Machine + "] " + title
		}

		records = append(records, store.Record{
			Source:    "clipboard",
			Timestamp: time.Unix(e.TS, 0),
			Title:     title,
			URL:       e.URL,
			Body:      e.Content,
			RawID:     e.Hash,
		})
	}
	return records, scanner.Err()
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const zoteroConnectorBase = "http://localhost:23119"

// zoteroConnector talks to the Zotero connector server running on localhost.
// Zotero must be open for the connector to accept requests.
type zoteroConnector struct {
	client *http.Client
}

func newZoteroConnector() *zoteroConnector {
	return &zoteroConnector{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// ping checks that the Zotero connector is running.
func (z *zoteroConnector) ping() error {
	resp, err := z.client.Get(zoteroConnectorBase + "/connector/ping")
	if err != nil {
		return fmt.Errorf("zotero connector unreachable (is Zotero open?): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("zotero connector ping: HTTP %d", resp.StatusCode)
	}
	return nil
}

// zoteroImportItem is the Zotero item format accepted by the connector API.
type zoteroImportItem struct {
	ItemType     string       `json:"itemType"`
	Title        string       `json:"title"`
	URL          string       `json:"url"`
	Date         string       `json:"date,omitempty"`
	WebsiteTitle string       `json:"websiteTitle,omitempty"`
	Tags         []zoteroTag  `json:"tags"`
}

type zoteroTag struct {
	Tag string `json:"tag"`
}

type saveItemsPayload struct {
	SessionID string             `json:"sessionID"`
	URI       string             `json:"uri"`
	Items     []zoteroImportItem `json:"items"`
}

// saveItems sends items to Zotero in batches, returning the total saved count.
// Batches of 50 with 300ms between each to avoid overwhelming the connector.
func (z *zoteroConnector) saveItems(items []zoteroImportItem) (int, error) {
	const batchSize = 50
	const batchDelay = 300 * time.Millisecond

	saved := 0
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		payload := saveItemsPayload{
			SessionID: fmt.Sprintf("mp-import-%d", time.Now().UnixNano()),
			URI:       "https://memory-palace/import",
			Items:     items[i:end],
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return saved, fmt.Errorf("marshal batch %d: %w", i/batchSize+1, err)
		}

		resp, err := z.client.Post(
			zoteroConnectorBase+"/connector/saveItems",
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			return saved, fmt.Errorf("saveItems batch %d: %w", i/batchSize+1, err)
		}
		resp.Body.Close()

		if resp.StatusCode != 200 && resp.StatusCode != 201 {
			return saved, fmt.Errorf("saveItems batch %d: HTTP %d", i/batchSize+1, resp.StatusCode)
		}

		saved += len(items[i:end])
		if end < len(items) {
			time.Sleep(batchDelay)
		}
	}
	return saved, nil
}

package web

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/kashfshah/memory-palace/extractors"
	"github.com/kashfshah/memory-palace/store"
)

// IndexUpdate is the SSE payload pushed to browsers when the index changes.
type IndexUpdate struct {
	Source string `json:"source"`
	Added  int    `json:"added"`
}

type sseClient chan string

// liveHub manages SSE subscribers and broadcasts index updates.
type liveHub struct {
	mu      sync.RWMutex
	clients map[sseClient]struct{}
}

func newLiveHub() *liveHub {
	return &liveHub{clients: make(map[sseClient]struct{})}
}

func (h *liveHub) subscribe() sseClient {
	ch := make(sseClient, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *liveHub) unsubscribe(ch sseClient) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *liveHub) broadcast(ev IndexUpdate) {
	data, _ := json.Marshal(ev)
	msg := "event: indexUpdate\ndata: " + string(data) + "\n\n"
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client — drop to avoid blocking broadcast
		}
	}
}

// statsCache is a short-TTL in-memory cache for the /api/stats response.
type statsCache struct {
	mu      sync.RWMutex
	payload []byte
	builtAt time.Time
}

const statsCacheTTL = 10 * time.Second

func (c *statsCache) get() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.builtAt.IsZero() && time.Since(c.builtAt) < statsCacheTTL {
		return c.payload
	}
	return nil
}

func (c *statsCache) set(payload []byte) {
	c.mu.Lock()
	c.payload = payload
	c.builtAt = time.Now()
	c.mu.Unlock()
}

func (c *statsCache) invalidate() {
	c.mu.Lock()
	c.builtAt = time.Time{}
	c.mu.Unlock()
}

// fastSources are re-indexed every 30 seconds — cheap, low-latency reads.
var fastSources = []string{"knowledgec", "safari_open_tabs", "safari_icloud_tabs"}

// mediumSources are re-indexed every 5 minutes.
var mediumSources = []string{"safari_history", "calendar", "reminders", "notes", "safari_reading_list"}

// handleEvents serves the SSE stream at /api/events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	// Send an initial heartbeat so the browser knows the stream is live.
	w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			w.Write([]byte(msg))
			flusher.Flush()
		case <-ticker.C:
			// Heartbeat keeps the connection alive through proxies.
			w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// runIndexer starts background indexing. Called as a goroutine from Start().
func (s *Server) runIndexer() {
	fast := time.NewTicker(30 * time.Second)
	medium := time.NewTicker(5 * time.Minute)
	defer fast.Stop()
	defer medium.Stop()

	// Warm medium sources once shortly after startup.
	time.AfterFunc(8*time.Second, func() { s.indexSources(mediumSources) })

	for {
		select {
		case <-fast.C:
			s.indexSources(fastSources)
		case <-medium.C:
			s.indexSources(mediumSources)
		}
	}
}

func (s *Server) indexSources(sources []string) {
	db, err := store.Open(s.dbPath)
	if err != nil {
		log.Printf("live: open db: %v", err)
		return
	}
	defer db.Close()

	anyAdded := false
	for _, src := range sources {
		ext, ok := extractors.Registry[src]
		if !ok {
			continue
		}
		records, err := ext.Extract()
		if err != nil {
			log.Printf("live: %s extract: %v", src, err)
			continue
		}
		records = extractors.SanitizeRecords(records)
		n, err := db.Upsert(src, records)
		if err != nil {
			log.Printf("live: %s upsert: %v", src, err)
			continue
		}
		if n > 0 {
			anyAdded = true
			s.hub.broadcast(IndexUpdate{Source: src, Added: n})
		}
	}
	if anyAdded {
		s.sCache.invalidate()
		if err := db.RebuildFTS(); err != nil {
			log.Printf("live: FTS rebuild: %v", err)
		}
	}
}

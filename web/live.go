package web

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
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

// SourceStatus records the indexer result for one source. Written by the
// indexer process to data/indexer-status.json; read by handleHealth.
type SourceStatus struct {
	LastRun    time.Time `json:"last_run,omitempty"`
	LastChange time.Time `json:"last_change,omitempty"`
	LastAdded  int       `json:"last_added,omitempty"`
	Error      string    `json:"error,omitempty"`
	OK         bool      `json:"ok"`
}

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

// runWatcher polls memory.db mtime every 15s and broadcasts indexUpdate whenever
// the dedicated indexer process has written new data. No TCC permissions needed —
// the web server only stats its own database file.
func (s *Server) runWatcher() {
	var lastMod time.Time

	// Capture initial mtime without broadcasting.
	if fi, err := os.Stat(s.dbPath); err == nil {
		lastMod = fi.ModTime()
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		fi, err := os.Stat(s.dbPath)
		now := time.Now()

		if err != nil {
			log.Printf("watcher: stat %s: %v", s.dbPath, err)
			s.statusMu.Lock()
			s.sourceStatus["watcher"] = SourceStatus{LastRun: now, OK: false, Error: err.Error()}
			s.statusMu.Unlock()
			continue
		}

		changed := fi.ModTime().After(lastMod)

		s.statusMu.Lock()
		prev := s.sourceStatus["watcher"]
		s.sourceStatus["watcher"] = SourceStatus{
			LastRun:    now,
			LastChange: func() time.Time {
				if changed {
					return fi.ModTime()
				}
				return prev.LastChange
			}(),
			OK: true,
		}
		s.statusMu.Unlock()

		if changed {
			lastMod = fi.ModTime()
			s.sCache.invalidate()
			s.hub.broadcast(IndexUpdate{Source: "indexer", Added: 0})
		}
	}
}

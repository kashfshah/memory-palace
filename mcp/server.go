// Package mcp implements a Model Context Protocol stdio server for memory-palace.
// Each user runs their own instance against their local memory.db — no shared
// infrastructure, no accounts. Claude Desktop spawns it as a subprocess.
//
// Protocol: JSON-RPC 2.0 over stdin/stdout, MCP spec version 2024-11-05.
//
// Tools exposed:
//   - search(query, limit?)       — FTS search across all indexed sources
//   - get_recent(source?, limit?) — most recent entries, optionally filtered
//   - get_stats()                 — index summary (counts, date range, sources)
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kashfshah/memory-palace/embedder"
	"github.com/kashfshah/memory-palace/store"
)

const protocolVersion = "2024-11-05"

// Server is the MCP stdio server.
type Server struct {
	dbPath   string
	embedBin string // path to mp-embed binary; empty disables search_semantic
	out      *json.Encoder
}

// New creates a Server that reads from stdin and writes to stdout.
// embedBin is the path to the mp-embed binary; pass "" to omit search_semantic.
func New(dbPath, embedBin string) *Server {
	return &Server{
		dbPath:   dbPath,
		embedBin: embedBin,
		out:      json.NewEncoder(os.Stdout),
	}
}

// Run reads JSON-RPC messages from stdin until EOF, dispatching each one.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg jsonRPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.writeError(nil, -32700, "parse error")
			continue
		}

		// Notifications have no id — process but don't respond.
		if msg.ID == nil {
			continue
		}

		switch msg.Method {
		case "initialize":
			s.handleInitialize(msg.ID)
		case "tools/list":
			s.handleToolsList(msg.ID)
		case "tools/call":
			s.handleToolsCall(msg.ID, msg.Params)
		default:
			s.writeError(msg.ID, -32601, "method not found: "+msg.Method)
		}
	}

	return scanner.Err()
}

// ── Protocol types ────────────────────────────────────────────────────────────

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func (s *Server) handleInitialize(id json.RawMessage) {
	s.writeResult(id, map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "memory-palace", "version": "1.0.0"},
	})
}

func (s *Server) handleToolsList(id json.RawMessage) {
	tools := []map[string]any{
		{
			"name":        "search",
			"description": "Search your personal memory index (Safari history, bookmarks, Calendar, Notes, Reminders, Zotero, clipboard) using full-text search.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Full-text search query. Supports FTS5 syntax: quotes for phrases, AND/OR/NOT, prefix*.",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of results to return (default 20, max 100).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_recent",
			"description": "Get the most recently indexed entries, optionally filtered by source.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": "Filter by source: safari_history, safari_bookmarks, safari_reading_list, calendar, notes, reminders, zotero, clipboard, archivebox, knowledgec.",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of results to return (default 20, max 100).",
					},
				},
			},
		},
		{
			"name":        "get_stats",
			"description": "Get summary statistics for the memory index: total records, per-source counts, oldest and newest entries, last index time.",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}

	if s.embedBin != "" {
		tools = append(tools, map[string]any{
			"name":        "search_semantic",
			"description": "Search your memory index using semantic similarity (vector embeddings). Finds conceptually related content even without exact keyword matches. Requires records to be pre-embedded with --embed.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language query. No special syntax needed.",
					},
					"limit": map[string]any{
						"type":        "number",
						"description": "Maximum number of results to return (default 10, max 50).",
					},
				},
				"required": []string{"query"},
			},
		})
	}

	s.writeResult(id, map[string]any{"tools": tools})
}

func (s *Server) handleToolsCall(id json.RawMessage, rawParams json.RawMessage) {
	var p toolCallParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		s.writeError(id, -32602, "invalid params")
		return
	}

	switch p.Name {
	case "search":
		s.toolSearch(id, p.Arguments)
	case "search_semantic":
		s.toolSearchSemantic(id, p.Arguments)
	case "get_recent":
		s.toolGetRecent(id, p.Arguments)
	case "get_stats":
		s.toolGetStats(id)
	default:
		s.writeError(id, -32602, "unknown tool: "+p.Name)
	}
}

// ── Tools ─────────────────────────────────────────────────────────────────────

func (s *Server) toolSearch(id json.RawMessage, args json.RawMessage) {
	var a struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
		s.writeToolError(id, "search requires a non-empty query")
		return
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.Limit > 100 {
		a.Limit = 100
	}

	results, err := store.Query(s.dbPath, a.Query)
	if err != nil {
		s.writeToolError(id, "search failed: "+err.Error())
		return
	}

	if len(results) > a.Limit {
		results = results[:a.Limit]
	}

	var sb strings.Builder
	if len(results) == 0 {
		sb.WriteString("No results found.")
	} else {
		fmt.Fprintf(&sb, "%d result(s) for %q:\n\n", len(results), a.Query)
		for i, r := range results {
			fmt.Fprintf(&sb, "%d. [%s] %s — %s\n",
				i+1, r.Source, r.Timestamp.Format("2006-01-02"), r.Title)
			if r.URL != "" {
				fmt.Fprintf(&sb, "   %s\n", r.URL)
			}
			if r.Body != "" {
				body := r.Body
				if len(body) > 200 {
					body = body[:200] + "…"
				}
				fmt.Fprintf(&sb, "   %s\n", body)
			}
		}
	}

	s.writeToolText(id, sb.String())
}

func (s *Server) toolGetRecent(id json.RawMessage, args json.RawMessage) {
	var a struct {
		Source string `json:"source"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		s.writeToolError(id, "invalid arguments")
		return
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	if a.Limit > 100 {
		a.Limit = 100
	}

	records, err := store.Recent(s.dbPath, a.Source, a.Limit)
	if err != nil {
		s.writeToolError(id, "get_recent failed: "+err.Error())
		return
	}

	var sb strings.Builder
	if len(records) == 0 {
		sb.WriteString("No entries found.")
	} else {
		label := "all sources"
		if a.Source != "" {
			label = a.Source
		}
		fmt.Fprintf(&sb, "%d most recent entries (%s):\n\n", len(records), label)
		for i, r := range records {
			fmt.Fprintf(&sb, "%d. [%s] %s — %s\n",
				i+1, r.Source, r.Timestamp.Format("2006-01-02 15:04"), r.Title)
			if r.URL != "" {
				fmt.Fprintf(&sb, "   %s\n", r.URL)
			}
		}
	}

	s.writeToolText(id, sb.String())
}

func (s *Server) toolGetStats(id json.RawMessage) {
	stats, err := store.Stats(s.dbPath)
	if err != nil {
		s.writeToolError(id, "stats failed: "+err.Error())
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Memory Palace Index\n")
	fmt.Fprintf(&sb, "Total records: %d\n", stats.Total)
	fmt.Fprintf(&sb, "Date range:    %s → %s\n",
		stats.Oldest.Format("2006-01-02"),
		stats.Newest.Format("2006-01-02"))
	if !stats.Built.IsZero() {
		fmt.Fprintf(&sb, "Last indexed:  %s\n", stats.Built.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&sb, "\nBy source:\n")
	for src, count := range stats.BySrc {
		fmt.Fprintf(&sb, "  %-25s %d\n", src, count)
	}

	s.writeToolText(id, sb.String())
}

func (s *Server) toolSearchSemantic(id json.RawMessage, args json.RawMessage) {
	if s.embedBin == "" {
		s.writeToolError(id, "semantic search not configured (no embed binary)")
		return
	}

	var a struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
		s.writeToolError(id, "search_semantic requires a non-empty query")
		return
	}
	if a.Limit <= 0 {
		a.Limit = 10
	}
	if a.Limit > 50 {
		a.Limit = 50
	}

	// Embed the query.
	emb, err := embedder.New(s.embedBin)
	if err != nil {
		s.writeToolError(id, "start embedder: "+err.Error())
		return
	}
	defer emb.Close()

	queryVec, err := emb.Embed(a.Query)
	if err != nil {
		s.writeToolError(id, "embed query: "+err.Error())
		return
	}

	// Load all stored embeddings and rank by cosine similarity.
	db, err := openDB(s.dbPath)
	if err != nil {
		s.writeToolError(id, "open db: "+err.Error())
		return
	}
	defer db.Close()

	stored, err := db.LoadEmbeddings()
	if err != nil {
		s.writeToolError(id, "load embeddings: "+err.Error())
		return
	}
	if len(stored) == 0 {
		s.writeToolText(id, "No embeddings found. Run: memory-palace --embed --db <path>")
		return
	}

	type scored struct {
		id    int64
		score float32
	}
	scores := make([]scored, len(stored))
	for i, sv := range stored {
		scores[i] = scored{sv.ID, embedder.Cosine(queryVec, sv.Vec)}
	}
	// Partial sort: find top-N without full sort.
	topN := a.Limit
	if topN > len(scores) {
		topN = len(scores)
	}
	for i := 0; i < topN; i++ {
		maxIdx := i
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[maxIdx].score {
				maxIdx = j
			}
		}
		scores[i], scores[maxIdx] = scores[maxIdx], scores[i]
	}

	ids := make([]int64, topN)
	for i := range ids {
		ids[i] = scores[i].id
	}

	records, err := db.RecordsByIDs(ids)
	if err != nil {
		s.writeToolError(id, "fetch records: "+err.Error())
		return
	}

	var sb strings.Builder
	if len(records) == 0 {
		sb.WriteString("No embedded results found.")
	} else {
		fmt.Fprintf(&sb, "%d semantic result(s) for %q:\n\n", len(records), a.Query)
		for i, r := range records {
			fmt.Fprintf(&sb, "%d. [%s] %s — %s  (similarity: %.3f)\n",
				i+1, r.Source, r.Timestamp.Format("2006-01-02"), r.Title, scores[i].score)
			if r.URL != "" {
				fmt.Fprintf(&sb, "   %s\n", r.URL)
			}
		}
	}
	s.writeToolText(id, sb.String())
}

// openDB opens the memory DB read-only for MCP queries.
func openDB(dbPath string) (*store.DB, error) {
	return store.Open(dbPath)
}

// ── Response helpers ──────────────────────────────────────────────────────────

func (s *Server) writeResult(id json.RawMessage, result any) {
	_ = s.out.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	})
}

func (s *Server) writeError(id json.RawMessage, code int, message string) {
	_ = s.out.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	})
}

func (s *Server) writeToolText(id json.RawMessage, text string) {
	s.writeResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	})
}

func (s *Server) writeToolError(id json.RawMessage, message string) {
	s.writeResult(id, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": "Error: " + message},
		},
		"isError": true,
	})
}


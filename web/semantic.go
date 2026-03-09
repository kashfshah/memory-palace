package web

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kashfshah/memory-palace/embedder"
)

// handleSearchSemantic serves GET /api/search/semantic?q=...&limit=N&source=...
// Embeds the query via the mp-embed subprocess, loads stored vectors from the DB,
// ranks by cosine similarity, and returns the top-N records with similarity scores.
func (s *Server) handleSearchSemantic(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	source := r.URL.Query().Get("source")

	// Embed the query using the local NLEmbedding subprocess.
	emb, err := embedder.New(s.embedBin)
	if err != nil {
		http.Error(w, "embedder unavailable: "+err.Error(), 503)
		return
	}
	defer emb.Close()

	queryVec, err := emb.Embed(q)
	if err != nil {
		http.Error(w, "embed query: "+err.Error(), 500)
		return
	}

	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	// Load stored embeddings, optionally filtered by source.
	query := "SELECT id, embedding FROM memory WHERE embedding IS NOT NULL"
	var args []any
	if source != "" {
		query += " AND source = ?"
		args = append(args, source)
	}
	rows, err := conn.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	type scored struct {
		id    int64
		score float32
	}
	var scores []scored
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		vec := blobToFloat32s(blob)
		if len(vec) > 0 {
			scores = append(scores, scored{id, embedder.Cosine(queryVec, vec)})
		}
	}
	rows.Close()

	if len(scores) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	// Partial sort: top-N without sorting the entire slice.
	topN := limit
	if topN > len(scores) {
		topN = len(scores)
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	scores = scores[:topN]

	// Build ID → similarity map and fetch full records.
	simByID := make(map[int64]float32, topN)
	ids := make([]int64, topN)
	for i, sc := range scores {
		ids[i] = sc.id
		simByID[sc.id] = sc.score
	}

	placeholders := strings.Repeat("?,", topN)
	placeholders = placeholders[:len(placeholders)-1]
	recArgs := make([]any, topN)
	for i, id := range ids {
		recArgs[i] = id
	}

	recRows, err := conn.Query(
		"SELECT id, source, timestamp, COALESCE(title,''), COALESCE(url,''), "+
			"COALESCE(body,''), COALESCE(summary,''), COALESCE(psh_tags,'') "+
			"FROM memory WHERE id IN ("+placeholders+")",
		recArgs...,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer recRows.Close()

	type dbRow struct {
		id     int64
		source string
		ts     int64
		title  string
		url    string
		body   string
		summary string
		pshTags string
	}
	byID := make(map[int64]dbRow, topN)
	for recRows.Next() {
		var row dbRow
		if err := recRows.Scan(&row.id, &row.source, &row.ts, &row.title, &row.url, &row.body, &row.summary, &row.pshTags); err != nil {
			continue
		}
		byID[row.id] = row
	}

	type Result struct {
		Source     string   `json:"source"`
		Time       string   `json:"time"`
		Unix       int64    `json:"unix"`
		Title      string   `json:"title"`
		URL        string   `json:"url"`
		Body       string   `json:"body"`
		Summary    string   `json:"summary,omitempty"`
		PSHTags    []string `json:"psh_tags,omitempty"`
		Similarity float32  `json:"similarity"`
	}

	results := make([]Result, 0, topN)
	for _, sc := range scores {
		row, ok := byID[sc.id]
		if !ok {
			continue
		}
		r := Result{
			Source:     row.source,
			Time:       time.Unix(row.ts, 0).Format("2006-01-02 15:04"),
			Unix:       row.ts,
			Title:      row.title,
			URL:        row.url,
			Body:       row.body,
			Summary:    row.summary,
			Similarity: sc.score,
		}
		if row.pshTags != "" {
			r.PSHTags = strings.Split(row.pshTags, ",")
		}
		results = append(results, r)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// blobToFloat32s decodes a little-endian float32 blob from the DB.
func blobToFloat32s(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

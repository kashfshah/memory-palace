package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// errDBMissing is returned by openDB when the database file does not yet exist.
var errDBMissing = errors.New("database not yet created — run memory-palace to build the index")

// Server serves the memory palace web UI.
type Server struct {
	dbPath       string
	port         int
	certFile     string
	keyFile      string
	authUser     string
	authPass     string
	hub          *liveHub
	sCache       statsCache
	statusMu     sync.RWMutex
	sourceStatus map[string]SourceStatus
}

// Option configures the web server.
type Option func(*Server)

// WithTLS enables HTTPS with the given cert and key files.
func WithTLS(certFile, keyFile string) Option {
	return func(s *Server) {
		s.certFile = certFile
		s.keyFile = keyFile
	}
}

// WithBasicAuth enables HTTP Basic Auth with the given credentials.
func WithBasicAuth(user, pass string) Option {
	return func(s *Server) {
		s.authUser = user
		s.authPass = pass
	}
}

// New creates a new web server.
func New(dbPath string, port int, opts ...Option) *Server {
	s := &Server{
		dbPath:       dbPath,
		port:         port,
		hub:          newLiveHub(),
		sourceStatus: make(map[string]SourceStatus),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Start launches the web server and background DB watcher.
func (s *Server) Start() error {
	go s.runWatcher()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealthPage)
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/timeline", s.handleTimeline)
	mux.HandleFunc("/api/domains", s.handleDomains)
	mux.HandleFunc("/api/clusters", s.handleClusters)
	mux.HandleFunc("/api/psh", s.handlePSH)
	mux.HandleFunc("/api/psh/items", s.handlePSHItems)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/health", s.handleHealth)

	var handler http.Handler = mux
	if s.authUser != "" {
		handler = s.basicAuth(handler)
	}

	addr := fmt.Sprintf(":%d", s.port)
	if s.certFile != "" && s.keyFile != "" {
		log.Printf("Memory Palace UI: https://localhost:%d (TLS + auth)", s.port)
		return http.ListenAndServeTLS(addr, s.certFile, s.keyFile, handler)
	}
	addr = fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("Memory Palace UI: http://%s", addr)
	return http.ListenAndServe(addr, handler)
}

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.authUser || pass != s.authPass {
			w.Header().Set("WWW-Authenticate", `Basic realm="Memory Palace"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) openDB() (*sql.DB, error) {
	if _, err := os.Stat(s.dbPath); os.IsNotExist(err) {
		return nil, errDBMissing
	}
	return sql.Open("sqlite", s.dbPath+"?mode=ro")
}

// openDBOrJSON opens the DB. On errDBMissing it writes a setup JSON response
// and returns (nil, false). On other errors it writes a 500. Returns (conn, true) on success.
func (s *Server) openDBOrJSON(w http.ResponseWriter, emptyJSON []byte) (*sql.DB, bool) {
	conn, err := s.openDB()
	if errors.Is(err, errDBMissing) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(emptyJSON)
		return nil, false
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return nil, false
	}
	return conn, true
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func (s *Server) handleHealthPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(healthHTML))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	source := r.URL.Query().Get("source")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	var err error
	var rows *sql.Rows
	if q != "" {
		query := `SELECT m.source, m.timestamp, COALESCE(m.title,''), COALESCE(m.url,''),
			COALESCE(m.body,''), COALESCE(m.summary,''), COALESCE(m.location,''), COALESCE(m.psh_tags,'')
			FROM memory m JOIN memory_fts f ON f.rowid = m.id
			WHERE memory_fts MATCH ?`
		args := []any{q}
		if source != "" {
			query += " AND m.source = ?"
			args = append(args, source)
		}
		query += " ORDER BY f.rank LIMIT ?"
		args = append(args, limit)
		rows, err = conn.Query(query, args...)
	} else {
		query := `SELECT source, timestamp, COALESCE(title,''), COALESCE(url,''),
			COALESCE(body,''), COALESCE(summary,''), COALESCE(location,''), COALESCE(psh_tags,'')
			FROM memory WHERE 1=1`
		var args []any
		if source != "" {
			query += " AND source = ?"
			args = append(args, source)
		}
		query += " ORDER BY timestamp DESC LIMIT ?"
		args = append(args, limit)
		rows, err = conn.Query(query, args...)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type Result struct {
		Source   string   `json:"source"`
		Time     string   `json:"time"`
		Unix     int64    `json:"unix"`
		Title    string   `json:"title"`
		URL      string   `json:"url"`
		Body     string   `json:"body"`
		Summary  string   `json:"summary"`
		Location string   `json:"location,omitempty"`
		PSHTags  []string `json:"psh_tags,omitempty"`
	}
	var results []Result
	for rows.Next() {
		var r Result
		var ts int64
		var pshTagsStr string
		if err := rows.Scan(&r.Source, &ts, &r.Title, &r.URL, &r.Body, &r.Summary, &r.Location, &pshTagsStr); err != nil {
			continue
		}
		r.Unix = ts
		r.Time = time.Unix(ts, 0).Format("2006-01-02 15:04")
		if pshTagsStr != "" {
			r.PSHTags = strings.Split(pshTagsStr, ",")
		}
		results = append(results, r)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Serve from cache if still fresh.
	if cached := s.sCache.get(); cached != nil {
		w.Write(cached)
		return
	}

	setupJSON, _ := json.Marshal(map[string]any{
		"total": 0, "by_source": map[string]int{}, "enriched": 0,
		"oldest": "", "newest": "", "setup": true,
	})
	conn, ok := s.openDBOrJSON(w, setupJSON)
	if !ok {
		return
	}
	defer conn.Close()

	type Stats struct {
		Total    int            `json:"total"`
		BySrc    map[string]int `json:"by_source"`
		Enriched int            `json:"enriched"`
		Oldest   string         `json:"oldest"`
		Newest   string         `json:"newest"`
	}
	st := Stats{BySrc: make(map[string]int)}

	conn.QueryRow("SELECT COUNT(*) FROM memory").Scan(&st.Total)

	rows, err := conn.Query("SELECT source, COUNT(*) FROM memory GROUP BY source")
	if err == nil {
		for rows.Next() {
			var src string
			var c int
			rows.Scan(&src, &c)
			st.BySrc[src] = c
		}
		rows.Close()
	}

	conn.QueryRow("SELECT COUNT(*) FROM memory WHERE summary IS NOT NULL AND summary <> ''").Scan(&st.Enriched)

	var oldest, newest int64
	conn.QueryRow("SELECT COALESCE(MIN(timestamp),0), COALESCE(MAX(timestamp),0) FROM memory WHERE timestamp > 0").Scan(&oldest, &newest)
	st.Oldest = time.Unix(oldest, 0).Format("2006-01-02")
	st.Newest = time.Unix(newest, 0).Format("2006-01-02")

	payload, _ := json.Marshal(st)
	s.sCache.set(payload)
	w.Write(payload)
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	source := r.URL.Query().Get("source")
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "week"
	}

	var format string
	switch granularity {
	case "day":
		format = "%Y-%m-%d"
	case "month":
		format = "%Y-%m"
	default:
		format = "%Y-W%W"
	}

	query := `SELECT strftime(?, datetime(timestamp, 'unixepoch')) as period,
		source, COUNT(*) as cnt
		FROM memory WHERE timestamp > 0`
	args := []any{format}
	if source != "" {
		query += " AND source = ?"
		args = append(args, source)
	}
	query += " GROUP BY period, source ORDER BY period"

	rows, err := conn.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type Point struct {
		Period string `json:"period"`
		Source string `json:"source"`
		Count  int    `json:"count"`
	}
	var points []Point
	for rows.Next() {
		var p Point
		rows.Scan(&p.Period, &p.Source, &p.Count)
		points = append(points, p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(points)
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	limitStr := r.URL.Query().Get("limit")
	limit := 30
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	rows, err := conn.Query(`
		SELECT
			CASE
				WHEN url LIKE 'https://%' THEN substr(url, 9, instr(substr(url,9),'/')-1)
				WHEN url LIKE 'http://%' THEN substr(url, 8, instr(substr(url,8),'/')-1)
				ELSE 'other'
			END as domain,
			COUNT(*) as cnt
		FROM memory
		WHERE url IS NOT NULL AND url <> '' AND url LIKE 'http%'
		GROUP BY domain
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type Domain struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var domains []Domain
	for rows.Next() {
		var d Domain
		rows.Scan(&d.Name, &d.Count)
		// Clean up empty domain names
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" {
			d.Name = "other"
		}
		domains = append(domains, d)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(domains)
}

func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	// Extract meaningful title words, then aggregate in Go for better filtering
	rows, err := conn.Query(`
		SELECT LOWER(title) as ltitle, source
		FROM memory
		WHERE title IS NOT NULL AND title <> '' AND LENGTH(title) > 5
		  AND title NOT LIKE 'http%'
		  AND title NOT LIKE '/%'
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "from": true, "with": true, "they": true,
		"been": true, "said": true, "each": true, "which": true, "their": true,
		"will": true, "other": true, "about": true, "more": true, "some": true,
		"than": true, "them": true, "into": true, "that": true, "this": true,
		"what": true, "your": true, "when": true, "make": true, "like": true,
		"does": true, "just": true, "over": true, "such": true, "take": true,
		"also": true, "back": true, "after": true, "only": true, "come": true,
		"could": true, "would": true, "should": true, "where": true, "there": true,
		"these": true, "those": true, "being": true, "between": true, "through": true,
		"while": true, "before": true, "under": true, "above": true,
		"having": true, "doing": true, "going": true, "making": true, "using": true,
		"getting": true, "looking": true, "finding": true, "working": true,
		"search": true, "untitled": true, "page": true, "home": true,
		"login": true, "error": true, "null": true, "undefined": true,
		"index": true, "pages": true, "inbox": true, "mail": true, "proton": true,
		"https": true, "http": true, "www": true, "com": true, "org": true,
		"news": true, "show": true, "tell": true, "new": true, "how": true,
	}

	wordCounts := map[string]int{}
	wordSources := map[string]map[string]bool{}

	for rows.Next() {
		var title, source string
		rows.Scan(&title, &source)

		// Split into words, count meaningful ones
		words := splitWords(title)
		for _, w := range words {
			if len(w) < 4 || stopWords[w] {
				continue
			}
			// Skip URLs, paths, numbers
			if strings.Contains(w, ".") || strings.Contains(w, "/") ||
				strings.Contains(w, ":") || strings.Contains(w, "@") {
				continue
			}
			if _, err := strconv.Atoi(w); err == nil {
				continue
			}
			wordCounts[w]++
			if wordSources[w] == nil {
				wordSources[w] = map[string]bool{}
			}
			wordSources[w][source] = true
		}
	}

	type Cluster struct {
		Topic   string   `json:"topic"`
		Count   int      `json:"count"`
		Sources []string `json:"sources"`
	}

	var clusters []Cluster
	for word, count := range wordCounts {
		if count < 5 {
			continue
		}
		var srcs []string
		for src := range wordSources[word] {
			srcs = append(srcs, src)
		}
		clusters = append(clusters, Cluster{Topic: word, Count: count, Sources: srcs})
	}

	// Sort by count descending
	for i := 0; i < len(clusters); i++ {
		for j := i + 1; j < len(clusters); j++ {
			if clusters[j].Count > clusters[i].Count {
				clusters[i], clusters[j] = clusters[j], clusters[i]
			}
		}
	}

	if len(clusters) > 60 {
		clusters = clusters[:60]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clusters)
}

// handlePSH returns the PSH section/sub-section tree with item counts.
// GET /api/psh → [{l1, total, subs: [{l2, count}]}] sorted by total desc.
func (s *Server) handlePSH(w http.ResponseWriter, r *http.Request) {
	conn, ok := s.openDBOrJSON(w, []byte("[]"))
	if !ok {
		return
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT l1, l2, COUNT(DISTINCT item_id) AS cnt
		FROM psh_sections
		GROUP BY l1, l2
		ORDER BY l1, cnt DESC
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	type Sub struct {
		L2    string `json:"l2"`
		Count int    `json:"count"`
	}
	type Section struct {
		L1    string `json:"l1"`
		Total int    `json:"total"`
		Subs  []Sub  `json:"subs"`
	}

	byL1 := map[string]*Section{}
	order := []string{}

	for rows.Next() {
		var l1 string
		var l2 *string
		var cnt int
		rows.Scan(&l1, &l2, &cnt)

		sec, ok := byL1[l1]
		if !ok {
			sec = &Section{L1: l1}
			byL1[l1] = sec
			order = append(order, l1)
		}
		sec.Total += cnt
		if l2 != nil {
			sec.Subs = append(sec.Subs, Sub{L2: *l2, Count: cnt})
		}
	}

	// Sort sections by total desc
	result := make([]Section, 0, len(order))
	for _, l1 := range order {
		result = append(result, *byL1[l1])
	}
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Total > result[i].Total {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handlePSHItems returns paginated items for a given l1 (and optionally l2).
// GET /api/psh/items?l1=informatics&l2=web+engineering&offset=0&limit=30
func (s *Server) handlePSHItems(w http.ResponseWriter, r *http.Request) {
	l1 := r.URL.Query().Get("l1")
	l2 := r.URL.Query().Get("l2")
	offsetStr := r.URL.Query().Get("offset")
	limitStr := r.URL.Query().Get("limit")
	if l1 == "" {
		http.Error(w, "l1 required", 400)
		return
	}
	offset := 0
	limit := 30
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 200 {
		limit = v
	}

	emptyJSON, _ := json.Marshal(map[string]any{"total": 0, "items": []any{}})
	conn, ok := s.openDBOrJSON(w, emptyJSON)
	if !ok {
		return
	}
	defer conn.Close()

	var err error
	var countRow *sql.Row
	var itemRows *sql.Rows

	if l2 == "" {
		countRow = conn.QueryRow(
			`SELECT COUNT(DISTINCT ps.item_id) FROM psh_sections ps WHERE ps.l1=?`, l1)
		itemRows, err = conn.Query(`
			SELECT DISTINCT m.id, m.title, m.url, m.source, m.timestamp, m.psh_tags
			FROM memory m JOIN psh_sections ps ON ps.item_id=m.id
			WHERE ps.l1=?
			ORDER BY m.timestamp DESC LIMIT ? OFFSET ?`,
			l1, limit, offset)
	} else {
		countRow = conn.QueryRow(
			`SELECT COUNT(DISTINCT ps.item_id) FROM psh_sections ps WHERE ps.l1=? AND ps.l2=?`, l1, l2)
		itemRows, err = conn.Query(`
			SELECT DISTINCT m.id, m.title, m.url, m.source, m.timestamp, m.psh_tags
			FROM memory m JOIN psh_sections ps ON ps.item_id=m.id
			WHERE ps.l1=? AND ps.l2=?
			ORDER BY m.timestamp DESC LIMIT ? OFFSET ?`,
			l1, l2, limit, offset)
	}

	var total int
	countRow.Scan(&total)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer itemRows.Close()

	type Item struct {
		ID        int      `json:"id"`
		Title     *string  `json:"title"`
		URL       *string  `json:"url"`
		Source    string   `json:"source"`
		Timestamp int64    `json:"timestamp"`
		PSHTags   []string `json:"psh_tags,omitempty"`
	}

	items := []Item{}
	for itemRows.Next() {
		var it Item
		var pshStr *string
		itemRows.Scan(&it.ID, &it.Title, &it.URL, &it.Source, &it.Timestamp, &pshStr)
		if pshStr != nil && *pshStr != "" {
			it.PSHTags = strings.Split(*pshStr, "|")
		}
		items = append(items, it)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total": total,
		"items": items,
	})
}

// handleHealth returns system health for the Memory Palace server.
// GET /api/health → {db, index, live_indexer}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	type DBInfo struct {
		OK     bool    `json:"ok"`
		SizeMB float64 `json:"size_mb,omitempty"`
		Error  string  `json:"error,omitempty"`
	}
	type IndexInfo struct {
		Total     int            `json:"total"`
		BySrc     map[string]int `json:"by_source"`
		LastBuild string         `json:"last_build,omitempty"`
	}
	type HealthResp struct {
		DB          DBInfo                  `json:"db"`
		Index       IndexInfo               `json:"index"`
		LiveSources map[string]SourceStatus `json:"live_indexer"`
	}

	resp := HealthResp{
		Index:       IndexInfo{BySrc: make(map[string]int)},
		LiveSources: make(map[string]SourceStatus),
	}

	// DB file check.
	fi, err := os.Stat(s.dbPath)
	if err != nil {
		resp.DB = DBInfo{OK: false, Error: err.Error()}
	} else {
		resp.DB = DBInfo{OK: true, SizeMB: float64(fi.Size()) / 1e6}
	}

	// Index stats (best-effort — skip if DB not ready).
	if conn, err := s.openDB(); err == nil {
		defer conn.Close()
		conn.QueryRow("SELECT COUNT(*) FROM memory").Scan(&resp.Index.Total)
		if rows, err := conn.Query("SELECT source, COUNT(*) FROM memory GROUP BY source"); err == nil {
			for rows.Next() {
				var src string
				var c int
				rows.Scan(&src, &c)
				resp.Index.BySrc[src] = c
			}
			rows.Close()
		}
		conn.QueryRow("SELECT value FROM meta WHERE key = 'last_build'").Scan(&resp.Index.LastBuild)
	}

	// Live indexer source status.
	s.statusMu.RLock()
	for k, v := range s.sourceStatus {
		resp.LiveSources[k] = v
	}
	s.statusMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func splitWords(s string) []string {
	var words []string
	var word []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '\'' {
			word = append(word, c)
		} else if len(word) > 0 {
			words = append(words, string(word))
			word = word[:0]
		}
	}
	if len(word) > 0 {
		words = append(words, string(word))
	}
	return words
}

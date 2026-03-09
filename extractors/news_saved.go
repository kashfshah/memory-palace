package extractors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kashfshah/memory-palace/store"
)

const (
	newsReadingListFile = "Library/Containers/com.apple.News/Data/Library/Application Support/com.apple.news/com.apple.news.public-com.apple.news.private-production/reading-list"
	defaultNewsCachePath = "data/news-url-cache.json"
	newsWorkers          = 10
	newsBatchDelay       = 60 * time.Millisecond
	newsHTTPTimeout      = 10 * time.Second
	newsUserAgent        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	reNewsRedirect = regexp.MustCompile(`redirectToUrl\("([^"]+)"\)`)
	reNewsTitle    = regexp.MustCompile(`<title>([^<]+)</title>`)
)

// NewsSaved extracts Apple News.app saved stories.
// Parses the NSKeyedArchiver binary reading-list file, resolves
// apple.news article IDs to canonical publisher URLs (with a persistent
// local cache), and returns one record per resolved article.
type NewsSaved struct{}

// newsCacheEntry holds a resolved apple.news article URL and title.
type newsCacheEntry struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func (n *NewsSaved) Extract() ([]store.Record, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, ErrNotConfigured
	}

	filePath := home + "/" + newsReadingListFile
	if _, err := os.Stat(filePath); err != nil {
		return nil, ErrNotConfigured
	}

	// Parse the NSKeyedArchiver binary via python3 plistlib.
	// The file has two bplist segments: the first (~offset 43) is NSKeyedArchiver
	// sync metadata; the second (~offset 618) is the plain article-ID dict we want.
	const parseScript = `
import sys, plistlib, json
with open(sys.argv[1], 'rb') as f:
    data = f.read()
first = data.find(b'bplist00')
idx   = data.find(b'bplist00', first + 8) if first >= 0 else -1
if idx < 0:
    sys.exit('second bplist segment not found')
pl = plistlib.loads(data[idx:])
out = []
for k, v in pl.items():
    if not isinstance(v, dict):
        continue
    aid = v.get('articleID', '')
    da  = v.get('dateAdded')
    if not aid:
        continue
    out.append({'articleID': aid, 'dateAdded': da.timestamp() if da else 0})
print(json.dumps(out))
`
	cmd := exec.Command("python3", "-c", parseScript, filePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("news_saved: parse binary: %w", err)
	}

	var raw []struct {
		ArticleID string  `json:"articleID"`
		DateAdded float64 `json:"dateAdded"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("news_saved: json decode: %w", err)
	}

	// Load persistent URL cache (survives across extraction runs).
	cachePath := os.Getenv("NEWS_URL_CACHE_PATH")
	if cachePath == "" {
		cachePath = defaultNewsCachePath
	}
	cache := newsLoadCache(cachePath)

	// Resolve article IDs not yet in the cache.
	var toResolve []string
	for _, a := range raw {
		if _, ok := cache[a.ArticleID]; !ok {
			toResolve = append(toResolve, a.ArticleID)
		}
	}
	if len(toResolve) > 0 {
		fmt.Fprintf(os.Stderr, "news_saved: resolving %d new article URLs...\n", len(toResolve))
		resolved := newsResolveURLs(toResolve)
		for id, entry := range resolved {
			cache[id] = entry
		}
		fmt.Fprintf(os.Stderr, "news_saved: resolved %d/%d\n", len(resolved), len(toResolve))
		newsSaveCache(cachePath, cache)
	}

	// Build records from cached entries.
	records := make([]store.Record, 0, len(raw))
	for _, a := range raw {
		entry, ok := cache[a.ArticleID]
		if !ok || entry.URL == "" {
			continue // unresolvable (Apple News+, removed, etc.) — skip
		}
		title := entry.Title
		if title == "" {
			title = entry.URL
		}
		records = append(records, store.Record{
			Source:    "news_saved",
			Timestamp: time.Unix(int64(a.DateAdded), 0),
			Title:     title,
			URL:       entry.URL,
			RawID:     a.ArticleID,
		})
	}

	return records, nil
}

// newsResolveURLs fetches canonical publisher URLs for the given apple.news
// article IDs concurrently (newsWorkers goroutines, newsBatchDelay between batches).
func newsResolveURLs(ids []string) map[string]newsCacheEntry {
	result := make(map[string]newsCacheEntry)
	mu := sync.Mutex{}

	// Do not follow HTTP redirects — we extract the canonical URL from the
	// inline JS redirectToUrl("...") call in the HTML response.
	client := &http.Client{
		Timeout: newsHTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for i := 0; i < len(ids); i += newsWorkers {
		end := i + newsWorkers
		if end > len(ids) {
			end = len(ids)
		}

		var wg sync.WaitGroup
		for _, id := range ids[i:end] {
			wg.Add(1)
			go func(articleID string) {
				defer wg.Done()
				entry := newsFetchEntry(client, articleID)
				if entry.URL != "" {
					mu.Lock()
					result[articleID] = entry
					mu.Unlock()
				}
			}(id)
		}
		wg.Wait()

		if end < len(ids) {
			time.Sleep(newsBatchDelay)
		}
	}

	return result
}

// newsFetchEntry resolves a single apple.news article ID to its canonical URL.
func newsFetchEntry(client *http.Client, articleID string) newsCacheEntry {
	req, err := http.NewRequest("GET", "https://apple.news/"+articleID, nil)
	if err != nil {
		return newsCacheEntry{}
	}
	req.Header.Set("User-Agent", newsUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return newsCacheEntry{}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return newsCacheEntry{}
	}

	html := string(body)
	urlMatch := reNewsRedirect.FindStringSubmatch(html)
	if urlMatch == nil {
		return newsCacheEntry{} // Apple News+ article or article removed
	}

	entry := newsCacheEntry{URL: urlMatch[1]}
	if titleMatch := reNewsTitle.FindStringSubmatch(html); titleMatch != nil {
		entry.Title = strings.TrimSpace(titleMatch[1])
	}
	return entry
}

func newsLoadCache(path string) map[string]newsCacheEntry {
	cache := make(map[string]newsCacheEntry)
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	json.Unmarshal(data, &cache) //nolint:errcheck
	return cache
}

func newsSaveCache(path string, cache map[string]newsCacheEntry) {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0644) //nolint:errcheck
}

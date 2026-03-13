package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// archiveBoxFeed finds URLs in Memory Palace (zotero + safari_bookmarks) that
// ArchiveBox has not yet archived, and submits them for background archiving.
//
// Archiving consumes significant CPU and disk per URL (wget, singlefile, readability,
// mercury, screenshots, yt-dlp). The feed monitors host pressure and stops if
// the machine becomes overloaded.
//
// Requires ARCHIVEBOX_SSH_HOST env var pointing to the host running ArchiveBox.
//
// Crash safety: ArchiveBox writes every submitted URL to core_snapshot at submission
// time (before archiving begins). The feed diffs against the live ArchiveBox SQLite,
// so it naturally skips already-submitted URLs — even across crashes and restarts.
// No separate state file is needed or maintained.
//
// Progress can be checked via:
//
//	ssh $ARCHIVEBOX_SSH_HOST 'tmux capture-pane -t archivebox-feed -p'

const (
	memoryDBPath    = "data/memory.db"
	archiveDBLocal  = "/tmp/archivebox-index.sqlite3"
	archiveDBRemote = "archivebox/home/archivebox/data/index.sqlite3"

	// Pressure thresholds for the ArchiveBox host
	maxLoadAvg    = 8.0  // load avg 1-min threshold (2x cores)
	minFreeDiskGB = 50.0 // minimum free disk in ArchiveBox container
	minFreeMemMB  = 2048 // minimum free+available RAM on ArchiveBox host
	defaultBatch  = 0    // 0 = no limit (submit all eligible URLs)
)

// archiveSSHHost returns the SSH hostname for the ArchiveBox server
// from the ARCHIVEBOX_SSH_HOST environment variable.
func archiveSSHHost() string {
	if h := os.Getenv("ARCHIVEBOX_SSH_HOST"); h != "" {
		return h
	}
	return "cabinet"
}

// skipPatterns filters out URLs that won't archive well.
var skipPatterns = []string{
	"127.0.0.1",
	"localhost",
	"192.168.",
	"10.0.",
	".local",
	"file://",
}

// skipExtensions filters out direct links to binary files.
var skipExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".svg", ".ico", ".webp",
	".mp3", ".mp4", ".wav", ".avi", ".mov", ".mkv",
	".zip", ".tar", ".gz", ".bz2", ".xz", ".rar",
	".exe", ".dmg", ".pkg", ".deb", ".rpm",
	".woff", ".woff2", ".ttf", ".eot",
}

// hostPressure holds resource usage readings from the ArchiveBox host.
type hostPressure struct {
	LoadAvg1   float64
	FreeMemMB  int
	FreeDiskGB float64
}

func (p hostPressure) String() string {
	return fmt.Sprintf("load=%.1f mem=%dMB free disk=%.0fGB free", p.LoadAvg1, p.FreeMemMB, p.FreeDiskGB)
}

// ok returns true if all pressure readings fall within safe thresholds.
func (p hostPressure) ok() bool {
	return p.LoadAvg1 < maxLoadAvg && p.FreeMemMB > minFreeMemMB && p.FreeDiskGB > minFreeDiskGB
}

// reason returns a human-readable explanation of which thresholds exceeded.
func (p hostPressure) reason() string {
	var reasons []string
	if p.LoadAvg1 >= maxLoadAvg {
		reasons = append(reasons, fmt.Sprintf("load %.1f >= %.1f", p.LoadAvg1, maxLoadAvg))
	}
	if p.FreeMemMB <= minFreeMemMB {
		reasons = append(reasons, fmt.Sprintf("free mem %dMB <= %dMB", p.FreeMemMB, minFreeMemMB))
	}
	if p.FreeDiskGB <= minFreeDiskGB {
		reasons = append(reasons, fmt.Sprintf("free disk %.0fGB <= %.0fGB", p.FreeDiskGB, minFreeDiskGB))
	}
	return strings.Join(reasons, ", ")
}

func runArchiveBoxFeed(dryRun bool, batchSize int) error {
	if batchSize <= 0 {
		batchSize = defaultBatch
	}

	// Step 1: Pre-flight pressure check
	fmt.Printf("Checking %s pressure...\n", archiveSSHHost())
	pressure, err := checkCabinetPressure()
	if err != nil {
		fmt.Printf("WARNING: could not read host pressure: %v (proceeding cautiously)\n", err)
	} else {
		fmt.Printf("Host: %s\n", pressure)
		if !pressure.ok() {
			fmt.Printf("ABORT: host under pressure (%s)\n", pressure.reason())
			fmt.Println("Wait for load to decrease or free resources before feeding.")
			return nil
		}
	}

	// Step 2: Get all archivable URLs from Memory Palace
	mpURLs, err := getMemoryPalaceURLs()
	if err != nil {
		return fmt.Errorf("read memory palace: %w", err)
	}
	fmt.Printf("Memory Palace: %d archivable URLs (zotero + safari_bookmarks)\n", len(mpURLs))

	// Step 3: Sync and read existing ArchiveBox URLs
	if err := syncArchiveBoxForFeed(); err != nil {
		return fmt.Errorf("sync archivebox db: %w", err)
	}
	abURLs, err := getArchiveBoxURLs()
	if err != nil {
		return fmt.Errorf("read archivebox: %w", err)
	}
	fmt.Printf("ArchiveBox: %d existing snapshots\n", len(abURLs))

	// Step 4: Diff — find URLs in Memory Palace but not in ArchiveBox
	newURLs := diffURLs(mpURLs, abURLs)
	fmt.Printf("New URLs to archive: %d\n", len(newURLs))

	if len(newURLs) == 0 {
		fmt.Println("Nothing to feed — ArchiveBox already has all Memory Palace URLs.")
		return nil
	}

	// Step 5: Filter out URLs that won't archive well
	toFeed := filterURLs(newURLs)
	fmt.Printf("After filtering (localhost, binaries, etc.): %d URLs to submit\n", len(toFeed))

	if len(toFeed) == 0 {
		fmt.Println("Nothing to feed — ArchiveBox already has all eligible Memory Palace URLs.")
		return nil
	}

	// Step 6: Apply batch limit (0 = no limit)
	if batchSize > 0 && len(toFeed) > batchSize {
		fmt.Printf("Limiting to batch size %d (of %d total)\n", batchSize, len(toFeed))
		toFeed = toFeed[:batchSize]
	}

	if dryRun {
		show := 20
		if len(toFeed) < show {
			show = len(toFeed)
		}
		fmt.Printf("\n[DRY RUN] Would feed %d URLs. First %d:\n", len(toFeed), show)
		for i, u := range toFeed[:show] {
			fmt.Printf("  %d. %s\n", i+1, u)
		}
		if len(toFeed) > show {
			fmt.Printf("  ... and %d more\n", len(toFeed)-show)
		}
		return nil
	}

	// Step 8: Check for existing archivebox-feed tmux session
	if feedSessionActive() {
		fmt.Printf("ABORT: archivebox-feed tmux session already running on %s.\n", archiveSSHHost())
		fmt.Printf("Check progress: ssh %s 'tmux capture-pane -t archivebox-feed -p'\n", archiveSSHHost())
		fmt.Println("Wait for it to finish before feeding more URLs.")
		return nil
	}

	// Step 9: Push URL list and launch background archiving.
	// ArchiveBox records all URLs in core_snapshot at submission time,
	// so the next feed run will naturally skip them via the diff.
	if err := feedToArchiveBox(toFeed); err != nil {
		return fmt.Errorf("feed to archivebox: %w", err)
	}

	fmt.Printf("\nSubmitted %d URLs to ArchiveBox (background processing on %s).\n", len(toFeed), archiveSSHHost())
	fmt.Printf("Check progress: ssh %s 'tmux capture-pane -t archivebox-feed -p'\n", archiveSSHHost())
	return nil
}

// checkCabinetPressure reads load average, free memory, and free disk from the ArchiveBox host.
func checkCabinetPressure() (hostPressure, error) {
	var p hostPressure

	// Single SSH call to read all three metrics
	cmd := exec.Command("ssh", archiveSSHHost(),
		"cat /proc/loadavg && free -m | awk '/^Mem:/{print $7}' && "+
			"incus exec archivebox -- df -BG /home/archivebox/data 2>/dev/null | awk 'NR==2{print $4}'",
	)
	out, err := cmd.Output()
	if err != nil {
		return p, fmt.Errorf("ssh %s: %w", archiveSSHHost(), err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return p, fmt.Errorf("unexpected output: got %d lines, want 3", len(lines))
	}

	// Parse load average (first field of /proc/loadavg)
	loadFields := strings.Fields(lines[0])
	if len(loadFields) > 0 {
		p.LoadAvg1, _ = strconv.ParseFloat(loadFields[0], 64)
	}

	// Parse free memory (available MB from free -m)
	p.FreeMemMB, _ = strconv.Atoi(strings.TrimSpace(lines[1]))

	// Parse free disk (e.g. "383G" → 383.0)
	diskStr := strings.TrimSpace(lines[2])
	diskStr = strings.TrimSuffix(diskStr, "G")
	p.FreeDiskGB, _ = strconv.ParseFloat(diskStr, 64)

	return p, nil
}

// feedSessionActive returns true if an archivebox-feed tmux session exists on the ArchiveBox host.
func feedSessionActive() bool {
	cmd := exec.Command("ssh", archiveSSHHost(), "tmux has-session -t archivebox-feed 2>/dev/null")
	return cmd.Run() == nil
}

// filterURLs removes URLs that won't archive well.
func filterURLs(urls []string) []string {
	var kept []string
	for _, u := range urls {
		if shouldSkipURL(u) {
			continue
		}
		kept = append(kept, u)
	}
	return kept
}

// shouldSkipURL returns true if the URL should not go to ArchiveBox.
func shouldSkipURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)

	// Skip local/private network URLs
	for _, pat := range skipPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}

	// Skip binary file extensions
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	path := strings.ToLower(parsed.Path)
	for _, ext := range skipExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}

// syncArchiveBoxForFeed pulls the ArchiveBox SQLite DB from the remote host to local /tmp.
func syncArchiveBoxForFeed() error {
	remoteTmp := "/tmp/archivebox-index.sqlite3"
	cmd := exec.Command("ssh", archiveSSHHost(),
		"incus", "file", "pull", archiveDBRemote, remoteTmp,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("incus file pull: %w", err)
	}

	cmd = exec.Command("rsync", "-az", "--timeout=30",
		archiveSSHHost()+":"+remoteTmp, archiveDBLocal,
	)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getMemoryPalaceURLs returns all HTTP URLs from zotero and safari_bookmarks sources,
// ordered by most recently added first (newest URLs more likely to still exist).
func getMemoryPalaceURLs() ([]string, error) {
	conn, err := sql.Open("sqlite", memoryDBPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query(`
		SELECT url FROM (
			SELECT DISTINCT url, MAX(timestamp) as latest
			FROM memory
			WHERE source IN ('zotero', 'safari_bookmarks')
			  AND url IS NOT NULL AND url != ''
			  AND url LIKE 'http%'
			GROUP BY url
		) ORDER BY latest DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		urls = append(urls, normalizeURLForFeed(u))
	}
	return urls, nil
}

// getArchiveBoxURLs returns all URLs currently in ArchiveBox.
func getArchiveBoxURLs() (map[string]bool, error) {
	conn, err := sql.Open("sqlite", archiveDBLocal+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	rows, err := conn.Query("SELECT url FROM core_snapshot")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	urls := make(map[string]bool)
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			continue
		}
		urls[normalizeURLForFeed(u)] = true
	}
	return urls, nil
}

// diffURLs returns URLs present in mpURLs but not in abURLs.
func diffURLs(mpURLs []string, abURLs map[string]bool) []string {
	var diff []string
	for _, u := range mpURLs {
		if !abURLs[u] {
			diff = append(diff, u)
		}
	}
	return diff
}

// normalizeURLForFeed strips trailing slashes for comparison.
func normalizeURLForFeed(u string) string {
	return strings.TrimRight(u, "/")
}

// feedToArchiveBox pushes a URL list into the ArchiveBox container and launches
// background archiving via tmux on the ArchiveBox host.
func feedToArchiveBox(urls []string) error {
	timestamp := time.Now().Unix()
	localTmp := fmt.Sprintf("/tmp/archivebox-feed-%d.txt", timestamp)
	remoteTmp := fmt.Sprintf("/tmp/archivebox-feed-%d.txt", timestamp)
	containerSource := fmt.Sprintf("/home/archivebox/data/sources/%d-memory-palace-feed.txt", timestamp)

	// Write local temp file
	f, err := os.Create(localTmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	for _, u := range urls {
		fmt.Fprintln(f, u)
	}
	f.Close()
	defer os.Remove(localTmp)

	// Copy to ArchiveBox host
	cmd := exec.Command("scp", localTmp, archiveSSHHost()+":"+remoteTmp)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp to host: %w", err)
	}

	// Push into the Incus container's sources directory
	cmd = exec.Command("ssh", archiveSSHHost(),
		"incus", "file", "push", remoteTmp,
		"archivebox"+containerSource,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("incus file push: %w", err)
	}

	// Launch archivebox add in a detached tmux session on the ArchiveBox host.
	// This runs in the background — archiving takes minutes to hours.
	archiveCmd := fmt.Sprintf(
		"cd /home/archivebox/data && /home/archivebox/.local/bin/archivebox add --parser url_list %s",
		containerSource,
	)
	tmuxCmd := fmt.Sprintf(
		"tmux new-session -d -s archivebox-feed "+
			"'incus exec archivebox -- su - archivebox -c \"%s\"'",
		archiveCmd,
	)
	cmd = exec.Command("ssh", archiveSSHHost(), tmuxCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launch tmux session: %w", err)
	}

	// Clean up remote tmp
	exec.Command("ssh", archiveSSHHost(), "rm", "-f", remoteTmp).Run()

	return nil
}


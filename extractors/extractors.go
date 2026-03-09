package extractors

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kashfshah/memory-palace/store"
)

// snapshotDB copies a SQLite database (and its WAL/SHM files) to /tmp
// so extractors can read without conflicting with running apps that hold
// WAL-mode locks. Returns the path to the copy and a cleanup function.
func snapshotDB(srcPath string) (string, func(), error) {
	base := filepath.Base(srcPath)
	dst := filepath.Join(os.TempDir(), "mp-"+base)

	// Copy main DB file
	if err := copyFile(srcPath, dst); err != nil {
		return "", nil, fmt.Errorf("snapshot %s: %w", base, err)
	}

	// Copy WAL and SHM if they exist (needed for consistent reads)
	for _, suffix := range []string{"-wal", "-shm"} {
		src := srcPath + suffix
		if _, err := os.Stat(src); err == nil {
			copyFile(src, dst+suffix)
		}
	}

	cleanup := func() {
		os.Remove(dst)
		os.Remove(dst + "-wal")
		os.Remove(dst + "-shm")
	}

	return dst, cleanup, nil
}

func copyFile(src, dst string) error {
	cmd := exec.Command("cp", src, dst)
	return cmd.Run()
}

// Extractor pulls records from a data source.
type Extractor interface {
	Extract() ([]store.Record, error)
}

// Registry maps source names to their extractors.
var Registry = map[string]Extractor{
	"safari_history":      &SafariHistory{},
	"safari_bookmarks":    &SafariBookmarks{},
	"safari_reading_list": &SafariReadingList{},
	"safari_open_tabs":    &SafariOpenTabs{},
	"safari_icloud_tabs":  &SafariICloudTabs{},
	"calendar":            &Calendar{},
	"reminders":           &Reminders{},
	"notes":               &Notes{},
	"zotero":              &Zotero{},
	"archivebox":          &ArchiveBox{},
	"knowledgec":          &KnowledgeC{},
}

// AllSources returns all registered source names.
func AllSources() []string {
	out := make([]string, 0, len(Registry))
	for k := range Registry {
		out = append(out, k)
	}
	return out
}

// ParseSources parses the --sources flag value into a list of source names.
func ParseSources(val string) []string {
	if val == "all" || val == "" {
		return AllSources()
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

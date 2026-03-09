package web

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MetalStats is a point-in-time snapshot of machine resource usage.
type MetalStats struct {
	Ts         int64   `json:"ts"`
	Load1      float64 `json:"load1"`
	Load5      float64 `json:"load5"`
	Load15     float64 `json:"load15"`
	MemUsedMB  int64   `json:"mem_used_mb"`
	MemTotalMB int64   `json:"mem_total_mb"`
	DiskUsedGB float64 `json:"disk_used_gb"`
	DiskFreeGB float64 `json:"disk_free_gb"`
	NetRxBps   int64   `json:"net_rx_bps"` // bytes/s since previous sample
	NetTxBps   int64   `json:"net_tx_bps"`
}

// metalCollector holds the last computed snapshot.
// current is guarded by mu (read by HTTP handlers, written by collector goroutine).
// prevNet* are only touched by the collector goroutine — no locking needed for those.
type metalCollector struct {
	mu          sync.RWMutex
	current     MetalStats
	prevNetRx   int64
	prevNetTx   int64
	prevNetTime time.Time
}

// runMetalCollector polls machine stats every 30s and appends each sample to
// data/metal-history.jsonl. Following the project's file-as-IPC principle,
// the history file is the only persistence layer — no in-memory ring buffer.
func (s *Server) runMetalCollector() {
	// Prime the network baseline so the first real sample has a delta to compute.
	s.metal.prevNetRx, s.metal.prevNetTx = netCounters()
	s.metal.prevNetTime = time.Now()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		snap := s.sampleMetal()

		s.metal.mu.Lock()
		s.metal.current = snap
		s.metal.mu.Unlock()

		histPath := filepath.Join(filepath.Dir(s.dbPath), "metal-history.jsonl")
		if line, err := json.Marshal(snap); err == nil {
			if f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				f.Write(append(line, '\n'))
				f.Close()
			}
		}
	}
}

func (s *Server) sampleMetal() MetalStats {
	snap := MetalStats{Ts: time.Now().Unix()}
	snap.Load1, snap.Load5, snap.Load15 = loadAvg()
	snap.MemTotalMB, snap.MemUsedMB = memStats()
	snap.DiskUsedGB, snap.DiskFreeGB = diskStats(filepath.Dir(s.dbPath))

	rx, tx := netCounters()
	elapsed := time.Since(s.metal.prevNetTime).Seconds()
	if elapsed > 0 && s.metal.prevNetTime.Unix() > 0 {
		snap.NetRxBps = int64(float64(rx-s.metal.prevNetRx) / elapsed)
		snap.NetTxBps = int64(float64(tx-s.metal.prevNetTx) / elapsed)
	}
	if snap.NetRxBps < 0 {
		snap.NetRxBps = 0
	}
	if snap.NetTxBps < 0 {
		snap.NetTxBps = 0
	}
	s.metal.prevNetRx = rx
	s.metal.prevNetTx = tx
	s.metal.prevNetTime = time.Now()

	return snap
}

// handleMetal serves current machine stats and recent history for sparklines.
// GET /api/metal?hours=24  → {"current":{...}, "history":[...]}
func (s *Server) handleMetal(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if h, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && h > 0 && h <= 168 {
		hours = h
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()

	s.metal.mu.RLock()
	current := s.metal.current
	s.metal.mu.RUnlock()

	var history []MetalStats
	histPath := filepath.Join(filepath.Dir(s.dbPath), "metal-history.jsonl")
	if data, err := os.ReadFile(histPath); err == nil {
		for _, line := range splitLines(string(data)) {
			if line == "" {
				continue
			}
			var snap MetalStats
			if json.Unmarshal([]byte(line), &snap) == nil && snap.Ts >= cutoff {
				history = append(history, snap)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"current": current,
		"history": history,
	})
}

// loadAvg reads 1/5/15-minute load averages.
// On macOS: sysctl -n vm.loadavg → "{ 1.38 1.67 1.86 }"
func loadAvg() (l1, l5, l15 float64) {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return
	}
	s := strings.Trim(strings.TrimSpace(string(out)), "{} \t")
	parts := strings.Fields(s)
	if len(parts) >= 3 {
		l1, _ = strconv.ParseFloat(parts[0], 64)
		l5, _ = strconv.ParseFloat(parts[1], 64)
		l15, _ = strconv.ParseFloat(parts[2], 64)
	}
	return
}

// memStats returns total and used RAM in MB.
// Uses sysctl hw.memsize for total and vm_stat for free page count.
func memStats() (totalMB, usedMB int64) {
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
			totalMB = n / (1024 * 1024)
		}
	}
	if totalMB == 0 {
		return
	}

	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return
	}

	var pageSize int64 = 16384 // macOS default on Apple Silicon
	var freePages, specPages int64

	for _, line := range strings.Split(string(out), "\n") {
		// Extract page size from the header line.
		if strings.HasPrefix(line, "Mach Virtual Memory Statistics") {
			if idx := strings.Index(line, "page size of "); idx >= 0 {
				rest := line[idx+13:]
				if end := strings.Index(rest, " bytes"); end >= 0 {
					if n, err := strconv.ParseInt(rest[:end], 10, 64); err == nil {
						pageSize = n
					}
				}
			}
		}
		trimmed := strings.TrimSpace(line)
		parsePages := func(prefix string) (int64, bool) {
			if !strings.HasPrefix(trimmed, prefix) {
				return 0, false
			}
			s := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)), ".")
			n, err := strconv.ParseInt(s, 10, 64)
			return n, err == nil
		}
		if n, ok := parsePages("Pages free:"); ok {
			freePages = n
		}
		if n, ok := parsePages("Pages speculative:"); ok {
			specPages = n
		}
	}

	freeMB := (freePages + specPages) * pageSize / (1024 * 1024)
	usedMB = totalMB - freeMB
	if usedMB < 0 {
		usedMB = 0
	}
	return
}

// diskStats returns used and free disk space in GB for the filesystem
// containing path. Uses syscall.Statfs — no subprocess.
func diskStats(path string) (usedGB, freeGB float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return
	}
	bs := uint64(stat.Bsize)
	total := stat.Blocks * bs
	free := stat.Bfree * bs
	usedGB = float64(total-free) / 1e9
	freeGB = float64(free) / 1e9
	return
}

// netCounters returns cumulative Rx/Tx byte counters for the primary interface.
// Tries en0 (macOS Wi-Fi/Ethernet) then en1, eth0 as fallbacks.
func netCounters() (rxBytes, txBytes int64) {
	for _, iface := range []string{"en0", "en1", "eth0", "ens3"} {
		out, err := exec.Command("netstat", "-ib", "-I", iface).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// The Link# row carries the interface-level cumulative counters.
			if !strings.Contains(fields[2], "Link#") {
				continue
			}
			rx, rxErr := strconv.ParseInt(fields[6], 10, 64)
			tx, txErr := strconv.ParseInt(fields[9], 10, 64)
			if rxErr == nil && txErr == nil && rx > 0 {
				return rx, tx
			}
		}
	}
	return
}

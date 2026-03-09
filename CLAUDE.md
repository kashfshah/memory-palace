# memory-palace — Project Instructions

Personal memory index. Extracts from 10+ sources, stores in SQLite, serves a
web UI at memory.kashifshah.net. Hourly launchd indexer + always-on web server.

---

## Design Philosophy — Everything is a File

All inter-process communication uses named files, not sockets or shared memory.
Each process owns its output files; other processes read them.

| File | Writer | Reader | Carries |
|---|---|---|---|
| `data/indexer-status.json` | indexer (main.go) | web server | per-source run results |
| `data/indexer-history.jsonl` | indexer (main.go) | web server | time-series delta counts |
| `data/metal-history.jsonl` | web server collector | web server HTTP handler | CPU/RAM/disk/net history |
| `data/clipboard.jsonl` | clipboard-monitor.py | indexer | clipboard content stream |
| `data/memory.db` | indexer | web server | the full memory index |
| `data/.last-counts` | run-memory-palace.sh | run-memory-palace.sh | per-source Zulip delta state |
| `data/.last-feed` | `--feed` mode | `--feed` mode | ArchiveBox feed watermark |

New subsystems default to file-based IPC. Sockets are opt-in exceptions that
require a documented performance justification.

---

## Key Architecture

- **Indexer** (`main.go`) — runs hourly via launchd. Extracts from all sources,
  upserts into `data/memory.db`, writes `indexer-status.json` + appends to
  `indexer-history.jsonl`. Requires Full Disk Access (FDA) to read Apple system DBs.
- **Web server** (`web/`) — always-on Go HTTP server on port 7703. Never extracts
  data directly (no TCC permissions needed). Watches `memory.db` mtime every 15s
  for SSE live updates. Serves `/health`, `/api/metal`, `/api/history`.
- **Clipboard monitor** (`scripts/clipboard-monitor.py`) — cross-platform poller
  (pbpaste on macOS, xclip/xsel/wl-paste on Linux). Writes `data/clipboard.jsonl`.
  Runs as launchd on gray-box, systemd --user on chromabook.

---

## Extractor Pattern

Every extractor in `extractors/` follows this contract:
1. Return `extractors.ErrNotConfigured` if the source is unavailable (no error noise)
2. Call `snapshotDB(dbPath)` before opening any SQLite file owned by another app —
   copies DB + WAL + SHM to /tmp, avoids WAL-mode lock conflicts
3. Return `[]store.Record` — upsert semantics handle deduplication via `(source, raw_id)`

Adding a new extractor: implement `Extract() ([]store.Record, error)`, register in
`extractors/extractors.go` Registry, add label to `SRC_LABELS` in `web/index.html.go`
and `web/health.html.go`.

---

## Sanitizer

Two-layer content sanitizer prevents blocked content from entering the index:
- **Extract-time** (`extractors/sanitize.go`): filters per record during extraction
- **Post-index** (`store/memory.go` + `main.go`): `DeleteBlocked()` runs after every
  index build to catch anything that slipped through

Blocked domains and title substrings live in `extractors/blocklist.go`. Run
`memory-palace --sanitize` to apply the blocklist to an existing DB without re-indexing.

---

## Launchd Services

- `net.kashifshah.memory-palace` — hourly indexer (installed via `scripts/install-memory-palace.sh`)
- `net.kashifshah.memory-palace-web` — web server (`--serve --port 7703`)
- `net.kashifshah.clipboard-monitor` — clipboard-monitor.py (installed via `scripts/install-clipboard-monitor.sh`)

---

## Environment

- **Full Disk Access required** for the indexer to read Apple system DBs (Safari,
  Calendar, Reminders, Notes). Grant to Terminal.app or the binary in System Settings.
- **No FDA needed** for the web server — it only stats and reads `data/memory.db`.
- API keys in `.dev.vars` (gitignored): `KAGI_API_KEY`, `ARCHIVEBOX_HOST`, etc.
- `run-memory-palace.sh` sources `.dev.vars` before invoking the indexer binary.

---

## Build and Run

```bash
go build -o bin/memory-palace .

# Index all sources
./bin/memory-palace --db data/memory.db -v

# Serve web UI
./bin/memory-palace --serve --port 7703 --db data/memory.db

# Query
./bin/memory-palace --query "neural networks" --db data/memory.db

# Apply sanitizer to existing DB
./bin/memory-palace --sanitize --db data/memory.db
```

---

## Error Handling Conventions

- Extraction errors: `log.Printf("WARN: ...")` + continue (never fatal — one bad
  source should not abort all others)
- File IPC writes (status.json, history.jsonl): `log.Printf("WARN: ...")` — best-effort
  telemetry, not fatal
- DB errors: fatal — a corrupt or missing DB is unrecoverable without user action

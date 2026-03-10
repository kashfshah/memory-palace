#!/bin/bash
# Memory Palace installer.
# Builds the binary, creates the .app bundle needed for Full Disk Access,
# installs launchd agents, and guides the user through the FDA grant.
#
# Usage: bash scripts/install-memory-palace.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/bin/memory-palace"
BUNDLE_APP="$HOME/Applications/MemoryPalace.app"
BUNDLE_BINARY="$BUNDLE_APP/Contents/MacOS/memory-palace"
BUNDLE_ID="net.kashifshah.memory-palace"
DB="$PROJECT_DIR/data/memory.db"

INDEXER_LABEL="net.kashifshah.memory-palace"
WEB_LABEL="net.kashifshah.memory-palace-web"
INDEXER_PLIST="$HOME/Library/LaunchAgents/${INDEXER_LABEL}.plist"
WEB_PLIST="$HOME/Library/LaunchAgents/${WEB_LABEL}.plist"

# Optional run wrapper — can provide pre/post-indexing hooks (e.g. notifications).
# Set RUN_WRAPPER env var to a script path before running the installer, or leave
# unset to run the bundle binary directly.
RUN_WRAPPER="${RUN_WRAPPER:-}"

bold() { printf '\033[1m%s\033[0m\n' "$*"; }
info() { printf '  %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m  %s\n' "$*"; }
warn() { printf '  \033[33m⚠\033[0m  %s\n' "$*"; }
die()  { printf '\033[31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

# ── 1. Build ──────────────────────────────────────────────────────────────────
bold "1. Building binary"
(cd "$PROJECT_DIR" && go build -o bin/memory-palace .) || die "go build failed"
ok "bin/memory-palace built"

# ── 2. Create / update .app bundle ───────────────────────────────────────────
bold "2. Creating app bundle"
mkdir -p "$BUNDLE_APP/Contents/MacOS"

cat > "$BUNDLE_APP/Contents/Info.plist" << 'PLISTEOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>memory-palace</string>
    <key>CFBundleIdentifier</key>
    <string>net.kashifshah.memory-palace</string>
    <key>CFBundleName</key>
    <string>Memory Palace</string>
    <key>CFBundleVersion</key>
    <string>1.0</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
PLISTEOF

cp "$BINARY" "$BUNDLE_BINARY"
chmod +x "$BUNDLE_BINARY"
# Sign with Apple Development cert so macOS uses bundle ID for TCC lookup.
# Ad-hoc signing is not trusted by TCC for FDA; a real cert is required.
# After signing, the user must toggle FDA off/on in System Settings once to
# refresh the csreq stored in system TCC to match the new signature.
SIGNING_CERT=$(security find-identity -v -p codesigning 2>/dev/null \
    | grep "Apple Development" | head -1 | sed 's/.*"\(.*\)"/\1/')
if [ -n "$SIGNING_CERT" ]; then
    codesign --sign "$SIGNING_CERT" --force --deep "$BUNDLE_APP" 2>/dev/null
    ok "~/Applications/MemoryPalace.app created and signed ($SIGNING_CERT)"
else
    codesign --sign - --force --deep "$BUNDLE_APP" 2>/dev/null
    warn "No Apple Development cert found — ad-hoc signed (FDA toggle refresh required)"
fi

# ── 3. Check / grant Full Disk Access ────────────────────────────────────────
bold "3. Full Disk Access"

check_fda() {
    # Try reading a protected file — succeeds only if FDA is granted.
    # This works for any bundle ID and doesn't require sudo.
    /usr/bin/sqlite3 \
        "$HOME/Library/Safari/History.db" \
        "SELECT 1 LIMIT 1;" \
        >/dev/null 2>&1
}

if check_fda; then
    ok "Full Disk Access already granted"
else
    warn "Full Disk Access not yet granted"
    echo ""
    open "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"
    # Native dialog so the user can confirm without needing a terminal stdin.
    osascript -e 'display dialog "Memory Palace needs Full Disk Access.\n\n1. Click the lock icon and authenticate\n2. Find Memory Palace in the list — or click + and select ~/Applications/MemoryPalace.app\n3. Toggle it ON\n\nClick OK when done (or Cancel to skip)." buttons {"Skip", "OK"} default button "OK" with title "Memory Palace Installer"' \
        >/dev/null 2>&1 || true
    if check_fda; then
        ok "Full Disk Access granted"
    else
        warn "FDA not detected — continuing anyway. Re-run installer if indexing fails."
    fi
fi

# ── 4. Install launchd plists ─────────────────────────────────────────────────
bold "4. Installing launchd plists"

if [ -n "$RUN_WRAPPER" ] && [ -f "$RUN_WRAPPER" ]; then
    INDEXER_PROGRAM="$RUN_WRAPPER"
    info "Indexer: using run wrapper: $RUN_WRAPPER"
else
    INDEXER_PROGRAM="$BUNDLE_BINARY"
    info "Indexer: running bundle binary directly"
fi

cat > "$INDEXER_PLIST" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${INDEXER_LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INDEXER_PROGRAM}</string>
        <string>--auto-zotero</string>
    </array>
    <key>WorkingDirectory</key>
    <string>${PROJECT_DIR}</string>
    <key>StartInterval</key>
    <integer>3600</integer>
    <key>StandardOutPath</key>
    <string>${PROJECT_DIR}/data/memory-palace.log</string>
    <key>StandardErrorPath</key>
    <string>${PROJECT_DIR}/data/memory-palace.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin</string>
    </dict>
</dict>
</plist>
PLISTEOF
ok "Installed: $INDEXER_PLIST"

cat > "$WEB_PLIST" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${WEB_LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${PROJECT_DIR}/scripts/run-web.sh</string>
    </array>
    <key>WorkingDirectory</key>
    <string>${PROJECT_DIR}</string>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${HOME}/Library/Logs/memory-palace-web.log</string>
    <key>StandardErrorPath</key>
    <string>${HOME}/Library/Logs/memory-palace-web.log</string>
</dict>
</plist>
PLISTEOF
ok "Installed: $WEB_PLIST"

# ── 5. Load services ──────────────────────────────────────────────────────────
bold "5. Loading launchd services"

load_service() {
    local label="$1" plist="$2"
    launchctl bootout "gui/$(id -u)/${label}" 2>/dev/null || true
    launchctl bootstrap "gui/$(id -u)" "$plist"
    ok "Loaded: $label"
}

load_service "$INDEXER_LABEL" "$INDEXER_PLIST"
load_service "$WEB_LABEL" "$WEB_PLIST"

# ── 6. MCP server — Claude Desktop config ────────────────────────────────────
bold "6. MCP server (Claude Desktop)"

CLAUDE_CONFIG_DIR="$HOME/Library/Application Support/Claude"
CLAUDE_CONFIG="$CLAUDE_CONFIG_DIR/claude_desktop_config.json"
MCP_ENTRY="{\"command\":\"${BUNDLE_BINARY}\",\"args\":[\"--mcp\",\"--db\",\"${DB}\"]}"

if [ -f "$CLAUDE_CONFIG" ]; then
    # Merge into existing config using python3 (ships with macOS).
    python3 - "$CLAUDE_CONFIG" "$MCP_ENTRY" << 'PYEOF'
import sys, json
config_path, entry_json = sys.argv[1], sys.argv[2]
entry = json.loads(entry_json)
with open(config_path) as f:
    config = json.load(f)
config.setdefault("mcpServers", {})["memory-palace"] = entry
with open(config_path, "w") as f:
    json.dump(config, f, indent=2)
PYEOF
    ok "MCP entry merged into existing Claude Desktop config"
else
    mkdir -p "$CLAUDE_CONFIG_DIR"
    python3 -c "
import sys, json
entry = json.loads(sys.argv[1])
config = {'mcpServers': {'memory-palace': entry}}
with open(sys.argv[2], 'w') as f:
    json.dump(config, f, indent=2)
" "$MCP_ENTRY" "$CLAUDE_CONFIG"
    ok "Claude Desktop config created: $CLAUDE_CONFIG"
fi
info "Restart Claude Desktop to pick up the memory-palace MCP server."

# ── 7. First run ───────────────────────────────────────────────────────────────
bold "7. Triggering first indexer run"
launchctl kickstart "gui/$(id -u)/${INDEXER_LABEL}" 2>/dev/null || \
    launchctl start "$INDEXER_LABEL" 2>/dev/null || true
ok "Indexer started"

echo ""
bold "Installation complete."
echo ""
info "Indexer runs hourly. Web UI: http://localhost:7703"
info "MCP: restart Claude Desktop — 'memory-palace' server will appear"
info "Logs:  tail -f $PROJECT_DIR/data/memory-palace.log"
info "Check: launchctl list | grep memory-palace"

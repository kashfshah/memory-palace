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
BUNDLE_ID="com.kashif.memory-palace"
DB="$PROJECT_DIR/data/memory.db"

INDEXER_LABEL="com.kashif.memory-palace"
WEB_LABEL="com.kashif.memory-palace-web"
INDEXER_PLIST="$HOME/Library/LaunchAgents/${INDEXER_LABEL}.plist"
WEB_PLIST="$HOME/Library/LaunchAgents/${WEB_LABEL}.plist"

# claude-control run wrapper (sends Zulip notifications after each run).
# Falls back to running the bundle binary directly if not present.
CLAUDE_CONTROL_DIR="$HOME/Projects/claude-control"
RUN_WRAPPER="$CLAUDE_CONTROL_DIR/scripts/run-memory-palace.sh"

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
    <string>com.kashif.memory-palace</string>
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
SIGNING_CERT="Apple Development: Kashif Shah (RWPECCHSNL)"
if security find-identity -v -p codesigning | grep -q "$SIGNING_CERT" 2>/dev/null; then
    codesign --sign "$SIGNING_CERT" --force --deep "$BUNDLE_APP" 2>/dev/null
    ok "~/Applications/MemoryPalace.app created and signed (Apple Development)"
else
    codesign --sign - --force --deep "$BUNDLE_APP" 2>/dev/null
    warn "Apple Development cert not found — ad-hoc signed (FDA toggle refresh required)"
fi

# ── 3. Check / grant Full Disk Access ────────────────────────────────────────
bold "3. Full Disk Access"

check_fda() {
    local sys_val
    sys_val=$(sudo /usr/bin/sqlite3 \
        "/Library/Application Support/com.apple.TCC/TCC.db" \
        "SELECT auth_value FROM access WHERE service='kTCCServiceSystemPolicyAllFiles' AND client='$BUNDLE_ID';" \
        2>/dev/null || echo "")
    [ "$sys_val" = "2" ]
}

if check_fda; then
    ok "Full Disk Access already granted"
else
    warn "Full Disk Access not yet granted"
    echo ""
    echo "  Opening System Settings → Privacy & Security → Full Disk Access"
    echo ""
    echo "  Steps:"
    echo "    1. Click the lock icon and authenticate"
    echo "    2. Click +"
    echo "    3. Navigate to ~/Applications and select MemoryPalace"
    echo "    4. Toggle it ON"
    echo ""
    open "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"
    echo "  Waiting for FDA grant (checking every 3s)..."
    echo "  Press Ctrl+C to skip and grant FDA manually later."
    echo ""
    ATTEMPTS=0
    while ! check_fda; do
        sleep 3
        ATTEMPTS=$((ATTEMPTS + 1))
        if [ $((ATTEMPTS % 10)) -eq 0 ]; then
            echo "  Still waiting... (grant FDA to 'Memory Palace' in System Settings)"
        fi
    done
    ok "Full Disk Access granted"
fi

# ── 4. Install launchd plists ─────────────────────────────────────────────────
bold "4. Installing launchd plists"

if [ -f "$RUN_WRAPPER" ]; then
    INDEXER_PROGRAM="$RUN_WRAPPER"
    info "Indexer: run wrapper with Zulip notifications"
else
    INDEXER_PROGRAM="$BUNDLE_BINARY"
    warn "claude-control not found — running bundle binary directly (no Zulip notifications)"
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
        <string>${BUNDLE_BINARY}</string>
        <string>--serve</string>
        <string>--port</string>
        <string>7703</string>
        <string>--db</string>
        <string>${DB}</string>
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
    <key>EnvironmentVariables</key>
    <dict>
        <key>ARCHIVEBOX_SSH_HOST</key>
        <string>cabinet</string>
        <key>ARCHIVEBOX_INCUS_CONTAINER</key>
        <string>archivebox</string>
        <key>ARCHIVEBOX_INCUS_PATH</key>
        <string>archivebox/home/archivebox/data/index.sqlite3</string>
    </dict>
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

# ── 6. First run ───────────────────────────────────────────────────────────────
bold "6. Triggering first indexer run"
launchctl kickstart "gui/$(id -u)/${INDEXER_LABEL}" 2>/dev/null || \
    launchctl start "$INDEXER_LABEL" 2>/dev/null || true
ok "Indexer started"

echo ""
bold "Installation complete."
echo ""
info "Indexer runs hourly. Web UI: http://localhost:7703"
info "Logs:  tail -f $PROJECT_DIR/data/memory-palace.log"
info "Check: launchctl list | grep memory-palace"

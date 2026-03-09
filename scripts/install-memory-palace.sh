#!/bin/zsh
# Install a launchd user agent that rebuilds the Memory Palace index hourly.
# Usage: bash scripts/install-memory-palace.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/memory-palace"
LABEL="com.kashif.memory-palace"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"

# Build the binary if missing
if [ ! -f "$BINARY" ]; then
    echo "Building memory-palace binary..."
    (cd "$PROJECT_DIR" && go build -o memory-palace .)
fi

echo "Installing launchd agent: $LABEL"

cat > "$PLIST" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${BINARY}</string>
        <string>-db</string>
        <string>${PROJECT_DIR}/data/memory.db</string>
        <string>-sources</string>
        <string>all</string>
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

# Unload if already loaded, then load
launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

echo "Installed: $PLIST"
echo "Schedule: every 60 minutes"
echo "Log: $PROJECT_DIR/data/memory-palace.log"
echo ""
echo "To run now:  launchctl kickstart gui/$(id -u)/${LABEL}"
echo "To uninstall: launchctl bootout gui/$(id -u)/${LABEL} && rm $PLIST"

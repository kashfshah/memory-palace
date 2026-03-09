#!/bin/bash
# Install the clipboard monitor launchd agent.
# Substitutes __PLACEHOLDER__ values in the plist template with real paths.
#
# Usage: bash scripts/install-clipboard-monitor.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
LABEL="net.kashifshah.clipboard-monitor"
TEMPLATE="$SCRIPT_DIR/${LABEL}.plist"
DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"
PYTHON3="$(command -v python3)"

sed \
    -e "s|__PROJECT_DIR__|${PROJECT_DIR}|g" \
    -e "s|__HOME__|${HOME}|g" \
    -e "s|__HOSTNAME__|$(hostname -s)|g" \
    -e "s|__PYTHON3__|${PYTHON3}|g" \
    "$TEMPLATE" > "$DEST"

launchctl bootout "gui/$(id -u)/${LABEL}" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$DEST"
echo "Installed and loaded: $LABEL"

#!/bin/bash
# Wrapper for memory-palace web server.
# Sources .dev.vars so env config stays out of the launchd plist.
# Called by com.kashif.memory-palace-web launchd agent.

set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUNDLE_BINARY="$HOME/Applications/MemoryPalace.app/Contents/MacOS/memory-palace"
DB="$PROJECT_DIR/data/memory.db"

# Load env vars from .dev.vars if present.
if [ -f "$PROJECT_DIR/.dev.vars" ]; then
    set -o allexport
    # shellcheck disable=SC1090
    source "$PROJECT_DIR/.dev.vars"
    set +o allexport
fi

exec "$BUNDLE_BINARY" --serve --port 7703 --db "$DB" --embed-bin "$PROJECT_DIR/bin/mp-embed"

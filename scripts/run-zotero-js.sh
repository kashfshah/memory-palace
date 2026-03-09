#!/bin/zsh
# Run a JavaScript file in Zotero's Run JavaScript console
# Usage: ./run-zotero-js.sh <script.js> [wait_seconds]

SCRIPT_FILE="$1"
WAIT=${2:-5}

if [ ! -f "$SCRIPT_FILE" ]; then
    echo "Usage: $0 <script.js> [wait_seconds]"
    exit 1
fi

SCRIPT_NAME=$(basename "$SCRIPT_FILE")
echo "=== Running $SCRIPT_NAME (wait: ${WAIT}s) ==="

# Copy script to clipboard
cat "$SCRIPT_FILE" | pbcopy

# Activate Zotero and open Run JavaScript
osascript <<'APPLESCRIPT'
tell application "Zotero" to activate
delay 0.5
tell application "System Events"
    tell process "Zotero"
        click menu item "Run JavaScript" of menu "Developer" of menu item "Developer" of menu "Tools" of menu bar 1
    end tell
end tell
APPLESCRIPT

sleep 1

# Ensure "Run as async function" checkbox is checked, then paste and run
osascript <<'APPLESCRIPT'
tell application "System Events"
    tell process "Zotero"
        -- Find the async checkbox in the Run JavaScript window
        set jsWin to window "Run JavaScript"
        set asyncCb to checkbox 1 of jsWin
        if value of asyncCb is 0 then
            click asyncCb
            delay 0.2
        end if
        -- Select all in code pane, paste, and run
        keystroke "a" using command down
        delay 0.2
        keystroke "v" using command down
        delay 0.3
        -- Cmd+R runs the script
        keystroke "r" using command down
    end tell
end tell
APPLESCRIPT

echo "  Submitted. Waiting ${WAIT}s..."
sleep "$WAIT"
echo "  Done."

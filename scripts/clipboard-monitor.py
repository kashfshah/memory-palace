#!/usr/bin/env python3
"""
clipboard-monitor.py — Cross-platform clipboard poller for Memory Palace.

Polls the system clipboard every POLL_INTERVAL seconds, detects changes,
and appends new entries to CLIPBOARD_JSONL_PATH as newline-delimited JSON.

macOS:  pbpaste
Linux:  xclip -o -selection clipboard (requires DISPLAY=:0)
        fallback: xsel --output --clipboard, wl-paste

Environment variables:
  CLIPBOARD_JSONL_PATH    Output path (default: data/clipboard.jsonl)
  CLIPBOARD_POLL_INTERVAL Seconds between polls (default: 5)
  CLIPBOARD_MIN_LENGTH    Minimum characters to capture (default: 10)
  CLIPBOARD_DEDUP_WINDOW  Seconds to suppress re-capture of same content (default: 3600)
  CLIPBOARD_MACHINE       Machine label written to each entry (default: hostname)
  DISPLAY                 X display for Linux (default: :0)
"""

import hashlib
import json
import os
import platform
import re
import subprocess
import sys
import time
from datetime import datetime, timezone
from typing import Optional

POLL_INTERVAL = int(os.environ.get("CLIPBOARD_POLL_INTERVAL", "5"))
MIN_LENGTH = int(os.environ.get("CLIPBOARD_MIN_LENGTH", "10"))
DEDUP_WINDOW = int(os.environ.get("CLIPBOARD_DEDUP_WINDOW", "3600"))
MACHINE = os.environ.get("CLIPBOARD_MACHINE", platform.node().split(".")[0])

_script_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUTPUT_PATH = os.environ.get(
    "CLIPBOARD_JSONL_PATH",
    os.path.join(_script_dir, "data", "clipboard.jsonl"),
)

_URL_RE = re.compile(r"https?://[^\s\"'<>]+")


def read_clipboard() -> Optional[str]:
    """Read current clipboard text. Returns None on failure or non-text content."""
    try:
        if platform.system() == "Darwin":
            result = subprocess.run(["pbpaste"], capture_output=True, timeout=3)
            if result.returncode != 0:
                return None
            return result.stdout.decode("utf-8", errors="replace")

        # Linux — inject DISPLAY so this works from a launchd/systemd context
        env = {**os.environ, "DISPLAY": os.environ.get("DISPLAY", ":0")}
        for cmd in (
            ["xclip", "-o", "-selection", "clipboard"],
            ["xsel", "--output", "--clipboard"],
            ["wl-paste", "--no-newline"],
        ):
            try:
                r = subprocess.run(cmd, capture_output=True, timeout=3, env=env)
                if r.returncode == 0:
                    return r.stdout.decode("utf-8", errors="replace")
            except FileNotFoundError:
                continue
        return None

    except subprocess.TimeoutExpired:
        return None
    except Exception:
        return None


def is_binary(text: str) -> bool:
    """Return True if text contains too many non-printable bytes (likely image/binary data)."""
    sample = text[:200]
    non_printable = sum(1 for c in sample if ord(c) < 9 or 13 < ord(c) < 32)
    return non_printable > len(sample) * 0.1


def content_hash(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()[:16]


def detect_url(text: str) -> str:
    m = _URL_RE.search(text)
    return m.group(0) if m else ""


def load_recent_hashes() -> set:
    """Load content hashes from entries written in the last DEDUP_WINDOW seconds."""
    if not os.path.exists(OUTPUT_PATH):
        return set()
    cutoff = time.time() - DEDUP_WINDOW
    hashes: set = set()
    try:
        with open(OUTPUT_PATH) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                    if entry.get("ts", 0) >= cutoff:
                        hashes.add(entry.get("hash", ""))
                except json.JSONDecodeError:
                    continue
    except OSError:
        pass
    return hashes


def append_entry(content: str, url: str, h: str) -> None:
    os.makedirs(os.path.dirname(OUTPUT_PATH), exist_ok=True)
    entry = {
        "ts": int(time.time()),
        "machine": MACHINE,
        "content": content,
        "url": url,
        "hash": h,
    }
    with open(OUTPUT_PATH, "a") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")


def main() -> None:
    print(f"clipboard-monitor starting", flush=True)
    print(f"  platform : {platform.system()}", flush=True)
    print(f"  machine  : {MACHINE}", flush=True)
    print(f"  output   : {OUTPUT_PATH}", flush=True)
    print(f"  poll     : {POLL_INTERVAL}s", flush=True)

    test = read_clipboard()
    if test is None:
        print("WARN: initial clipboard read returned None — check DISPLAY or clipboard tool", flush=True)

    last_hash = ""
    recent_hashes = load_recent_hashes()

    while True:
        try:
            content = read_clipboard()

            if not content or len(content) < MIN_LENGTH or is_binary(content):
                time.sleep(POLL_INTERVAL)
                continue

            content = content.strip()
            if not content:
                time.sleep(POLL_INTERVAL)
                continue

            h = content_hash(content)
            if h == last_hash or h in recent_hashes:
                time.sleep(POLL_INTERVAL)
                continue

            url = detect_url(content)
            append_entry(content, url, h)
            last_hash = h
            recent_hashes.add(h)

            label = url if url else content[:80].replace("\n", " ")
            print(f"[{datetime.now(timezone.utc).strftime('%H:%M:%S')}] {label}", flush=True)

        except KeyboardInterrupt:
            print("clipboard-monitor stopped", flush=True)
            sys.exit(0)
        except Exception as e:
            print(f"ERROR: {e}", flush=True)

        time.sleep(POLL_INTERVAL)


if __name__ == "__main__":
    main()

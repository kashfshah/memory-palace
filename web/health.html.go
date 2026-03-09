package web

const healthHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Memory Palace — Health</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>&#x1f3db;</text></svg>">
<style>
  :root {
    --bg: #0d1117; --surface: #161b22; --surface-hover: #1c2333;
    --border: #30363d; --border-focus: #58a6ff;
    --text: #e6edf3; --text-dim: #8b949e; --text-faint: #484f58;
    --accent: #58a6ff; --accent-dim: #1f6feb;
    --green: #3fb950; --orange: #d29922; --purple: #bc8cff;
    --red: #f85149; --cyan: #79c0ff;
    --radius: 8px; --radius-sm: 6px;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
    background: var(--bg); color: var(--text); line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }

  .container { max-width: 860px; margin: 0 auto; padding: 0 1rem 3rem; }

  header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 0; border-bottom: 1px solid var(--border); margin-bottom: 1.75rem;
  }
  .header-left { display: flex; align-items: center; gap: 0.75rem; }
  header h1 { font-size: 1.15rem; font-weight: 600; }
  header h1 span { color: var(--accent); }
  .header-right { display: flex; align-items: center; gap: 1rem; }
  .back-link {
    font-size: 0.8rem; color: var(--text-dim); text-decoration: none;
    display: flex; align-items: center; gap: 0.3rem;
  }
  .back-link:hover { color: var(--accent); }
  .refresh-info { font-size: 0.75rem; color: var(--text-faint); }

  /* Status badge */
  .badge {
    display: inline-flex; align-items: center; gap: 0.35rem;
    padding: 0.15rem 0.6rem; border-radius: 999px; font-size: 0.72rem;
    font-weight: 600; letter-spacing: 0.02em;
  }
  .badge-ok   { background: rgba(63,185,80,0.15);  color: var(--green); }
  .badge-err  { background: rgba(248,81,73,0.15);  color: var(--red); }
  .badge-warn { background: rgba(210,153,34,0.15); color: var(--orange); }
  .badge-dot  { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }

  /* Cards */
  .cards { display: flex; flex-direction: column; gap: 1.25rem; }
  .card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); overflow: hidden;
  }
  .card-header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 0.875rem 1.25rem; border-bottom: 1px solid var(--border);
  }
  .card-title {
    font-size: 0.8rem; font-weight: 600; letter-spacing: 0.04em;
    text-transform: uppercase; color: var(--text-dim);
  }
  .card-body { padding: 1.25rem; }

  /* DB card */
  .db-row { display: flex; align-items: center; gap: 1.5rem; flex-wrap: wrap; }
  .db-stat { display: flex; flex-direction: column; gap: 0.15rem; }
  .db-stat-val { font-size: 1.4rem; font-weight: 600; line-height: 1; }
  .db-stat-lbl { font-size: 0.72rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.04em; }
  .db-error {
    margin-top: 0.75rem; padding: 0.6rem 0.75rem;
    background: rgba(248,81,73,0.08); border: 1px solid rgba(248,81,73,0.2);
    border-radius: var(--radius-sm); color: var(--red); font-size: 0.8rem; font-family: monospace;
  }

  /* Index card */
  .index-meta { display: flex; gap: 2rem; flex-wrap: wrap; margin-bottom: 1.25rem; }
  .meta-item { display: flex; flex-direction: column; gap: 0.15rem; }
  .meta-val { font-size: 1.5rem; font-weight: 600; line-height: 1; }
  .meta-lbl { font-size: 0.72rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.04em; }

  .source-bars { display: flex; flex-direction: column; gap: 0.5rem; }
  .src-row {
    display: grid; grid-template-columns: 140px 1fr 70px; align-items: center; gap: 0.75rem;
  }
  .src-name { font-size: 0.8rem; color: var(--text-dim); white-space: nowrap; }
  .src-bar-wrap { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .src-bar { height: 100%; border-radius: 3px; background: var(--accent-dim); transition: width 0.4s; }
  .src-count { font-size: 0.8rem; text-align: right; color: var(--text-dim); font-variant-numeric: tabular-nums; }

  /* Live indexer table */
  .indexer-table { width: 100%; border-collapse: collapse; font-size: 0.82rem; }
  .indexer-table th {
    text-align: left; padding: 0 0.5rem 0.6rem;
    font-size: 0.7rem; font-weight: 600; text-transform: uppercase;
    letter-spacing: 0.04em; color: var(--text-faint); border-bottom: 1px solid var(--border);
  }
  .indexer-table td { padding: 0.6rem 0.5rem; border-bottom: 1px solid var(--border); vertical-align: top; }
  .indexer-table tr:last-child td { border-bottom: none; }
  .indexer-table tr:hover td { background: var(--surface-hover); }

  .status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; }
  .dot-ok      { background: var(--green); }
  .dot-err     { background: var(--red); }
  .dot-unknown { background: var(--text-faint); }

  .src-label { font-weight: 500; }
  .added-pos { color: var(--green); font-weight: 600; }
  .added-zero { color: var(--text-faint); }
  .err-text { color: var(--red); font-size: 0.75rem; font-family: monospace; word-break: break-all; }
  .time-dim { color: var(--text-dim); }

  .empty-row td { color: var(--text-faint); font-style: italic; text-align: center; padding: 2rem; }

  /* Loading skeleton */
  .skeleton { background: var(--surface); border-radius: var(--radius); }
  @keyframes shimmer { 0% { opacity: 0.5; } 50% { opacity: 1; } 100% { opacity: 0.5; } }
  .loading { animation: shimmer 1.5s infinite; color: var(--text-faint); padding: 2rem; text-align: center; }

  @media (max-width: 600px) {
    .src-row { grid-template-columns: 110px 1fr 55px; }
    .indexer-table th:nth-child(3),
    .indexer-table td:nth-child(3) { display: none; }
  }
</style>
</head>
<body>
<div class="container">
  <header>
    <div class="header-left">
      <h1><span>Memory</span> Palace &mdash; Health</h1>
    </div>
    <div class="header-right">
      <span class="refresh-info" id="refresh-info"></span>
      <a class="back-link" href="/">&#8592; Back to search</a>
    </div>
  </header>

  <div class="cards" id="cards">
    <div class="loading">Loading health data&hellip;</div>
  </div>
</div>
<script>
const SRC_LABELS = {
  safari_history:      'Safari History',
  safari_bookmarks:    'Bookmarks',
  safari_reading_list: 'Reading List',
  safari_open_tabs:    'Open Tabs',
  safari_icloud_tabs:  'iCloud Tabs',
  calendar:            'Calendar',
  reminders:           'Reminders',
  notes:               'Notes',
  zotero:              'Zotero',
  archivebox:          'ArchiveBox',
  knowledgec:          'App Usage',
  clipboard:           'Clipboard',
};

function srcLabel(s) { return SRC_LABELS[s] || s.replace(/_/g, ' '); }

function fmtNum(n) { return Number(n).toLocaleString(); }

function relTime(iso) {
  if (!iso) return '—';
  const diff = (Date.now() - new Date(iso)) / 1000;
  if (diff < 5)   return 'just now';
  if (diff < 60)  return Math.floor(diff) + 's ago';
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function badge(ok, label) {
  const cls = ok ? 'badge-ok' : 'badge-err';
  return '<span class="badge ' + cls + '"><span class="badge-dot"></span>' + label + '</span>';
}

function renderDB(db) {
  const b = badge(db.ok, db.ok ? 'OK' : 'ERROR');
  const size = db.size_mb ? '<div class="db-stat"><div class="db-stat-val">' + db.size_mb.toFixed(1) + ' MB</div><div class="db-stat-lbl">File Size</div></div>' : '';
  const err = db.error ? '<div class="db-error">' + escHtml(db.error) + '</div>' : '';
  return '<div class="card">' +
    '<div class="card-header"><span class="card-title">Database</span>' + b + '</div>' +
    '<div class="card-body">' +
      '<div class="db-row">' + size + '</div>' + err +
    '</div>' +
  '</div>';
}

function renderIndex(idx) {
  const lastBuild = idx.last_build ? relTime(idx.last_build) + ' <span style="color:var(--text-faint);font-size:0.7rem">(' + new Date(idx.last_build).toLocaleString() + ')</span>' : '—';
  const meta = '<div class="index-meta">' +
    '<div class="meta-item"><div class="meta-val">' + fmtNum(idx.total) + '</div><div class="meta-lbl">Total Records</div></div>' +
    '<div class="meta-item"><div class="meta-val" style="font-size:1rem">' + lastBuild + '</div><div class="meta-lbl">Last Build</div></div>' +
  '</div>';

  const bySrc = idx.by_source || {};
  const maxCount = Math.max(1, ...Object.values(bySrc));
  const sorted = Object.entries(bySrc).sort((a, b) => b[1] - a[1]);
  const bars = sorted.map(([src, count]) =>
    '<div class="src-row">' +
      '<div class="src-name">' + srcLabel(src) + '</div>' +
      '<div class="src-bar-wrap"><div class="src-bar" style="width:' + (count / maxCount * 100).toFixed(1) + '%"></div></div>' +
      '<div class="src-count">' + fmtNum(count) + '</div>' +
    '</div>'
  ).join('');

  return '<div class="card">' +
    '<div class="card-header"><span class="card-title">Index</span></div>' +
    '<div class="card-body">' + meta +
      '<div class="source-bars">' + bars + '</div>' +
    '</div>' +
  '</div>';
}

function renderIndexer(sources) {
  const entries = Object.entries(sources).sort((a, b) => a[0].localeCompare(b[0]));

  let rows = '';
  if (entries.length === 0) {
    rows = '<tr class="empty-row"><td colspan="5">No indexer runs yet — wait up to 30 seconds</td></tr>';
  } else {
    for (const [src, s] of entries) {
      const dot = s.ok
        ? '<span class="status-dot dot-ok" title="OK"></span>'
        : (s.error === 'not configured'
            ? '<span class="status-dot dot-unknown" title="Not configured"></span>'
            : '<span class="status-dot dot-err" title="Error"></span>');

      const added = s.last_added > 0
        ? '<span class="added-pos">+' + s.last_added + '</span>'
        : '<span class="added-zero">—</span>';

      const errCell = s.error && s.error !== 'not configured'
        ? '<span class="err-text">' + escHtml(s.error) + '</span>'
        : (s.error === 'not configured' ? '<span class="time-dim">not configured</span>' : '');

      rows += '<tr>' +
        '<td>' + dot + '</td>' +
        '<td class="src-label">' + srcLabel(src) + '</td>' +
        '<td class="time-dim">' + relTime(s.last_run) + '</td>' +
        '<td>' + added + '</td>' +
        '<td>' + errCell + '</td>' +
      '</tr>';
    }
  }

  // Overall status
  const total = entries.length;
  const failing = entries.filter(([, s]) => !s.ok && s.error !== 'not configured').length;
  const unconfigured = entries.filter(([, s]) => s.error === 'not configured').length;
  const statusBadge = failing > 0
    ? badge(false, failing + ' failing')
    : (total > 0 ? badge(true, 'All OK') : '<span class="badge badge-warn"><span class="badge-dot"></span>Warming up</span>');

  let legend = '';
  if (unconfigured > 0 || failing > 0) {
    legend = '<div style="margin-top:0.75rem;font-size:0.75rem;color:var(--text-faint);display:flex;gap:1rem;flex-wrap:wrap">';
    if (failing > 0)      legend += '<span><span class="status-dot dot-err" style="display:inline-block"></span> Error</span>';
    if (unconfigured > 0) legend += '<span><span class="status-dot dot-unknown" style="display:inline-block"></span> Not configured</span>';
    legend += '<span><span class="status-dot dot-ok" style="display:inline-block"></span> OK</span></div>';
  }

  return '<div class="card">' +
    '<div class="card-header"><span class="card-title">Live Indexer</span>' + statusBadge + '</div>' +
    '<div class="card-body" style="padding:0">' +
      '<table class="indexer-table"><thead><tr>' +
        '<th></th><th>Source</th><th>Last Run</th><th>Added</th><th>Detail</th>' +
      '</tr></thead><tbody>' + rows + '</tbody></table>' +
      (legend ? '<div style="padding:0.75rem 1.25rem">' + legend + '</div>' : '') +
    '</div>' +
  '</div>';
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

let lastFetch = null;

async function loadHealth() {
  try {
    const data = await fetch('/api/health').then(r => r.json());
    lastFetch = new Date();

    document.getElementById('cards').innerHTML =
      renderDB(data.db || {ok: false, error: 'no data'}) +
      renderIndex(data.index || {}) +
      renderIndexer(data.live_indexer || {});
  } catch(e) {
    document.getElementById('cards').innerHTML =
      '<div class="loading" style="color:var(--red)">Failed to load health data: ' + escHtml(e.message) + '</div>';
  }
}

function updateRefreshInfo() {
  const el = document.getElementById('refresh-info');
  if (!lastFetch) { el.textContent = ''; return; }
  const diff = Math.round((Date.now() - lastFetch) / 1000);
  el.textContent = 'Updated ' + diff + 's ago';
}

loadHealth();
setInterval(loadHealth, 30000);
setInterval(updateRefreshInfo, 1000);
</script>
</body>
</html>`

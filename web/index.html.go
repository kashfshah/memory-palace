package web

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Memory Palace</title>
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 100 100'><text y='.9em' font-size='90'>&#x1f3db;</text></svg>">
<style>
  :root {
    --bg: #0d1117; --surface: #161b22; --surface-hover: #1c2333;
    --border: #30363d; --border-focus: #58a6ff;
    --text: #e6edf3; --text-dim: #8b949e; --text-faint: #484f58;
    --accent: #58a6ff; --accent-dim: #1f6feb;
    --green: #3fb950; --orange: #d29922; --purple: #bc8cff;
    --red: #f85149; --pink: #f778ba; --cyan: #79c0ff; --lime: #7ee787;
    --radius: 8px; --radius-sm: 6px;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Noto Sans', Helvetica, Arial, sans-serif;
    background: var(--bg); color: var(--text); line-height: 1.5;
    -webkit-font-smoothing: antialiased;
  }

  /* Layout */
  .container { max-width: 1200px; margin: 0 auto; padding: 0.75rem 1rem; }

  header {
    display: flex; align-items: center; justify-content: space-between;
    padding: 1rem 0; border-bottom: 1px solid var(--border); margin-bottom: 1rem;
  }
  .header-left { display: flex; align-items: center; gap: 0.75rem; }
  header h1 { font-size: 1.25rem; font-weight: 600; letter-spacing: -0.01em; }
  header h1 span { color: var(--accent); }
  .header-meta { font-size: 0.75rem; color: var(--text-dim); }

  /* Stats bar */
  .stats-bar {
    display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr));
    gap: 0.5rem; margin-bottom: 1rem;
  }
  .stat {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.6rem 0.75rem;
    transition: border-color 0.15s;
  }
  .stat:hover { border-color: var(--text-faint); }
  .stat-value { font-size: 1.25rem; font-weight: 700; color: var(--accent); font-variant-numeric: tabular-nums; }
  .stat-label { font-size: 0.7rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.05em; }

  /* Skeleton loading */
  .skeleton { background: linear-gradient(90deg, var(--surface) 25%, var(--surface-hover) 50%, var(--surface) 75%);
    background-size: 200% 100%; animation: shimmer 1.5s ease-in-out infinite;
    border-radius: var(--radius-sm); }
  @keyframes shimmer { 0% { background-position: 200% 0; } 100% { background-position: -200% 0; } }
  .skeleton-stat { height: 3.5rem; }
  .skeleton-result { height: 4.5rem; margin-bottom: 0.5rem; }
  .skeleton-chart { height: 200px; }

  /* Search bar */
  .search-bar {
    display: flex; gap: 0.5rem; margin-bottom: 1rem;
    position: sticky; top: 0; z-index: 10; background: var(--bg);
    padding: 0.5rem 0;
  }
  .search-wrap { flex: 1; position: relative; }
  .search-wrap input {
    width: 100%; background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.6rem 2.5rem 0.6rem 1rem;
    color: var(--text); font-size: 0.95rem; outline: none;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .search-wrap input:focus { border-color: var(--border-focus); box-shadow: 0 0 0 3px rgba(88,166,255,0.15); }
  .search-wrap input::placeholder { color: var(--text-faint); }
  .search-clear {
    position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
    background: none; border: none; color: var(--text-dim); cursor: pointer;
    font-size: 1.1rem; display: none; padding: 0.2rem;
  }
  .search-clear.visible { display: block; }
  .search-wrap .kbd {
    position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
    font-size: 0.65rem; color: var(--text-faint); border: 1px solid var(--border);
    border-radius: 3px; padding: 0.1rem 0.35rem; pointer-events: none;
  }
  .search-wrap .kbd.hidden { display: none; }

  .search-bar select {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.6rem 0.5rem;
    color: var(--text); font-size: 0.85rem; cursor: pointer;
  }
  .search-bar button {
    background: var(--accent-dim); color: var(--text); border: none;
    border-radius: var(--radius-sm); padding: 0.6rem 1.2rem;
    font-weight: 600; cursor: pointer; font-size: 0.9rem;
    transition: background 0.15s;
  }
  .search-bar button:hover { background: var(--accent); color: #000; }

  /* Tabs */
  .tabs {
    display: flex; gap: 0; border-bottom: 1px solid var(--border); margin-bottom: 1rem;
    overflow-x: auto; -webkit-overflow-scrolling: touch;
  }
  .tab {
    padding: 0.6rem 1rem; cursor: pointer; color: var(--text-dim);
    border-bottom: 2px solid transparent; font-size: 0.85rem;
    transition: all 0.15s; white-space: nowrap; user-select: none;
  }
  .tab:hover { color: var(--text); background: var(--surface); }
  .tab.active { color: var(--accent); border-bottom-color: var(--accent); }

  .panel { display: none; }
  .panel.active { display: block; animation: fadeIn 0.2s ease; }
  @keyframes fadeIn { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: none; } }

  /* Results */
  .results { display: flex; flex-direction: column; gap: 0.4rem; }
  .result {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); padding: 0.7rem 0.9rem;
    transition: border-color 0.15s, background 0.15s;
  }
  .result:hover { border-color: var(--text-faint); background: var(--surface-hover); }
  .result-header { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.2rem; flex-wrap: wrap; }
  .result-source {
    font-size: 0.65rem; padding: 0.1rem 0.4rem; border-radius: 3px;
    font-weight: 600; text-transform: uppercase; letter-spacing: 0.03em;
  }
  .src-safari_history { background: rgba(88,166,255,0.15); color: var(--accent); }
  .src-safari_bookmarks { background: rgba(63,185,80,0.15); color: var(--green); }
  .src-calendar { background: rgba(210,153,34,0.15); color: var(--orange); }
  .src-reminders { background: rgba(188,140,255,0.15); color: var(--purple); }
  .src-notes { background: rgba(247,120,186,0.15); color: var(--pink); }
  .src-zotero { background: rgba(204,51,51,0.15); color: #cc3333; }
  .src-archivebox { background: rgba(240,136,62,0.15); color: #f0883e; }
  .src-safari_open_tabs { background: rgba(121,192,255,0.15); color: var(--cyan); }
  .src-safari_icloud_tabs { background: rgba(86,211,100,0.15); color: #56d364; }
  .src-safari_reading_list { background: rgba(56,139,253,0.15); color: #388bfd; }
  .src-knowledgec { background: rgba(163,113,247,0.15); color: #a371f7; }
  .result-time { font-size: 0.7rem; color: var(--text-faint); }
  .result-title { font-weight: 500; font-size: 0.95rem; }
  .result-title a { color: var(--text); text-decoration: none; transition: color 0.15s; }
  .result-title a:hover { color: var(--accent); }
  .result-body {
    font-size: 0.8rem; color: var(--text-dim); margin-top: 0.2rem;
    overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .result-summary {
    font-size: 0.8rem; color: var(--green); margin-top: 0.3rem;
    border-left: 2px solid var(--green); padding-left: 0.5rem;
    overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .result-location { font-size: 0.75rem; color: var(--orange); margin-top: 0.15rem; }
  .psh-tags { display: flex; flex-wrap: wrap; gap: 0.25rem; margin-top: 0.3rem; }
  .psh-tag { display: inline-block; padding: 0.1rem 0.4rem; background: rgba(30,60,90,0.5); border: 1px solid rgba(88,166,255,0.25);
    border-radius: 3px; font-size: 0.7rem; color: var(--accent); cursor: pointer; }
  .psh-tag:hover { background: rgba(88,166,255,0.15); border-color: rgba(88,166,255,0.5); }
  .result-count {
    font-size: 0.8rem; color: var(--text-dim); margin-bottom: 0.5rem;
    padding-bottom: 0.5rem; border-bottom: 1px solid var(--border);
  }

  .empty-state {
    text-align: center; padding: 3rem 1rem; color: var(--text-dim);
  }
  .empty-state .icon { font-size: 2.5rem; margin-bottom: 0.5rem; }
  .empty-state p { max-width: 400px; margin: 0 auto; font-size: 0.9rem; }

  .error-state {
    text-align: center; padding: 2rem; color: var(--red);
    background: rgba(248,81,73,0.1); border-radius: var(--radius);
  }
  .error-state button {
    margin-top: 0.75rem; background: var(--surface); border: 1px solid var(--border);
    color: var(--text); padding: 0.4rem 1rem; border-radius: var(--radius-sm);
    cursor: pointer;
  }

  /* Charts */
  .chart-container {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 1rem; margin-bottom: 1rem;
  }
  .chart-title {
    font-size: 0.85rem; font-weight: 600; margin-bottom: 0.75rem;
    color: var(--text-dim); display: flex; align-items: center; gap: 0.5rem;
  }
  .chart-controls {
    display: flex; gap: 0.25rem; margin-left: auto;
  }
  .chart-btn {
    background: none; border: 1px solid var(--border); color: var(--text-dim);
    border-radius: 4px; padding: 0.2rem 0.5rem; font-size: 0.7rem; cursor: pointer;
  }
  .chart-btn.active { background: var(--accent-dim); color: var(--text); border-color: var(--accent-dim); }

  /* Source cards grid */
  .source-grid {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 0.5rem; margin-bottom: 1rem;
  }
  .source-card {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius); padding: 0.75rem; cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
  }
  .source-card:hover { border-color: var(--border-focus); background: var(--surface-hover); }
  .source-card-value { font-size: 1.5rem; font-weight: 700; font-variant-numeric: tabular-nums; }
  .source-card-label { font-size: 0.7rem; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.05em; margin-top: 0.15rem; }
  .source-card-hint { font-size: 0.65rem; color: var(--text-faint); margin-top: 0.25rem; }

  .bar-chart { display: flex; flex-direction: column; gap: 0.25rem; }
  .bar-row { display: flex; align-items: center; gap: 0.5rem; cursor: pointer; border-radius: var(--radius-sm); padding: 0.15rem 0.25rem; transition: background 0.12s; }
  .bar-row:hover { background: var(--surface-hover); }
  .bar-label {
    width: 160px; font-size: 0.78rem; color: var(--text-dim); text-align: right;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 0;
  }
  .bar-track { flex: 1; height: 22px; background: rgba(255,255,255,0.03); border-radius: 3px; overflow: hidden; }
  .bar {
    height: 100%; border-radius: 3px; transition: width 0.4s ease;
    min-width: 2px; display: flex; align-items: center; padding-left: 6px;
  }
  .bar-inner-label { font-size: 0.65rem; color: rgba(0,0,0,0.7); font-weight: 600; white-space: nowrap; }
  .bar-value { font-size: 0.72rem; color: var(--text-dim); min-width: 45px; text-align: right; font-variant-numeric: tabular-nums; }

  .bubble-chart { display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; padding: 0.5rem 0; }
  .bubble {
    display: inline-flex; align-items: center; gap: 0.3rem;
    background: var(--surface-hover); border: 1px solid var(--border);
    border-radius: 999px; padding: 0.3rem 0.7rem;
    font-size: 0.78rem; cursor: pointer; transition: all 0.15s;
    color: var(--text-dim);
  }
  .bubble:hover { border-color: var(--accent); color: var(--accent); transform: translateY(-1px); }
  .bubble .count { font-size: 0.65rem; font-weight: 700; color: var(--accent); }
  .bubble .dot {
    display: inline-block; width: 6px; height: 6px; border-radius: 50%;
    flex-shrink: 0;
  }

  .timeline-chart { width: 100%; overflow-x: auto; }
  .timeline-chart svg { display: block; }
  .timeline-legend { display: flex; gap: 0.75rem; margin-top: 0.5rem; flex-wrap: wrap; }
  .timeline-legend-item { font-size: 0.72rem; display: flex; align-items: center; gap: 0.3rem; color: var(--text-dim); }
  .timeline-legend-dot { display: inline-block; width: 8px; height: 8px; border-radius: 2px; }

  /* PSH Navigator */
  .psh-accordion { display: flex; flex-direction: column; gap: 0.4rem; }
  .psh-section {
    background: var(--surface); border: 1px solid var(--border);
    border-radius: var(--radius-sm); overflow: hidden;
    transition: border-color 0.15s;
  }
  .psh-section.open { border-color: var(--text-faint); }
  .psh-section-header {
    display: flex; align-items: center; gap: 0.5rem;
    padding: 0.65rem 0.9rem; cursor: pointer;
    transition: background 0.15s; user-select: none;
  }
  .psh-section-header:hover { background: var(--surface-hover); }
  .psh-chevron { font-size: 0.6rem; color: var(--text-faint); width: 12px; transition: transform 0.2s; flex-shrink: 0; }
  .psh-section.open .psh-chevron { transform: rotate(90deg); }
  .psh-section-name { font-weight: 500; font-size: 0.9rem; flex: 1; }
  .psh-section-count { font-size: 0.75rem; color: var(--text-dim); font-variant-numeric: tabular-nums; }

  .psh-section-body { padding: 0.6rem 0.9rem; border-top: 1px solid var(--border); }
  .psh-bars { display: flex; flex-direction: column; gap: 0.2rem; margin-bottom: 0.5rem; }
  .psh-bar-row {
    display: flex; align-items: center; gap: 0.5rem; cursor: pointer; border-radius: 4px; padding: 0.1rem 0.25rem;
    transition: background 0.12s;
  }
  .psh-bar-row:hover { background: rgba(255,255,255,0.04); }
  .psh-bar-row.selected { background: rgba(88,166,255,0.08); }
  .psh-bar-label {
    width: 180px; font-size: 0.76rem; color: var(--text-dim); text-align: right;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 0;
  }
  .psh-bar-track { flex: 1; height: 18px; background: rgba(255,255,255,0.03); border-radius: 3px; overflow: hidden; }
  .psh-bar-fill {
    height: 100%; border-radius: 3px; background: var(--accent-dim);
    transition: width 0.35s ease; min-width: 2px;
    display: flex; align-items: center; padding-left: 5px;
  }
  .psh-bar-row.selected .psh-bar-fill { background: var(--accent); }
  .psh-bar-count { font-size: 0.7rem; color: var(--text-dim); min-width: 36px; text-align: right; font-variant-numeric: tabular-nums; }

  .psh-items { margin-top: 0.6rem; border-top: 1px solid var(--border); padding-top: 0.6rem; }
  .psh-items-header { font-size: 0.75rem; color: var(--text-faint); margin-bottom: 0.4rem; }
  .psh-load-more {
    text-align: center; margin-top: 0.5rem;
    background: none; border: 1px solid var(--border); color: var(--text-dim);
    border-radius: var(--radius-sm); padding: 0.4rem 1rem; cursor: pointer;
    font-size: 0.8rem; width: 100%; transition: border-color 0.15s;
  }
  .psh-load-more:hover { border-color: var(--text-faint); color: var(--text); }

  /* Responsive */
  @media (max-width: 768px) {
    .container { padding: 0.5rem; }
    header { flex-direction: column; align-items: flex-start; gap: 0.5rem; }
    .stats-bar { grid-template-columns: repeat(3, 1fr); }
    .search-bar { flex-wrap: wrap; }
    .search-wrap { min-width: 100%; }
    .search-bar select { flex: 1; }
    .search-bar button { flex: 1; }
    .bar-label { width: 100px; font-size: 0.7rem; }
    .tabs { gap: 0; }
    .tab { padding: 0.5rem 0.7rem; font-size: 0.8rem; }
  }
  @media (max-width: 480px) {
    .stats-bar { grid-template-columns: repeat(2, 1fr); }
    .stat-value { font-size: 1rem; }
    .bar-label { width: 80px; }
  }
</style>
</head>
<body>
<div class="container">
  <header>
    <div class="header-left">
      <h1><span>Memory</span> Palace</h1>
    </div>
    <div class="header-meta" id="header-meta"></div>
  </header>

  <div class="stats-bar" id="stats-bar">
    <div class="stat skeleton skeleton-stat"></div>
    <div class="stat skeleton skeleton-stat"></div>
    <div class="stat skeleton skeleton-stat"></div>
    <div class="stat skeleton skeleton-stat"></div>
    <div class="stat skeleton skeleton-stat"></div>
  </div>

  <div class="search-bar">
    <div class="search-wrap">
      <input type="text" id="search-input" placeholder="Search your memory..." autofocus>
      <span class="kbd" id="search-kbd">/</span>
      <button class="search-clear" id="search-clear" onclick="clearSearch()">&times;</button>
    </div>
    <select id="source-filter">
      <option value="">All sources</option>
      <option value="safari_history">Safari History</option>
      <option value="safari_bookmarks">Bookmarks</option>
      <option value="safari_open_tabs">Open Tabs</option>
      <option value="safari_icloud_tabs">iCloud Tabs</option>
      <option value="safari_reading_list">Reading List</option>
      <option value="calendar">Calendar</option>
      <option value="reminders">Reminders</option>
      <option value="notes">Notes</option>
      <option value="zotero">Zotero</option>
      <option value="archivebox">ArchiveBox</option>
      <option value="knowledgec">App Usage</option>
    </select>
    <button onclick="doSearch()">Search</button>
  </div>

  <div class="tabs">
    <div class="tab active" data-panel="results">Results</div>
    <div class="tab" data-panel="timeline">Timeline</div>
    <div class="tab" data-panel="domains">Domains</div>
    <div class="tab" data-panel="clusters">Topics</div>
    <div class="tab" data-panel="psh">PSH</div>
    <div class="tab" data-panel="sources">Sources</div>
  </div>

  <div class="panel active" id="results">
    <div class="empty-state">
      <div class="icon">&#x1f3db;</div>
      <p>Search your personal memory or browse recent activity across Safari, Calendar, Notes, Reminders, and Bookmarks.</p>
    </div>
  </div>

  <div class="panel" id="timeline">
    <div class="chart-container">
      <div class="chart-title">
        Activity Over Time
        <div class="chart-controls">
          <button class="chart-btn" onclick="loadTimeline('day')">Day</button>
          <button class="chart-btn" onclick="loadTimeline('week')">Week</button>
          <button class="chart-btn active" onclick="loadTimeline('month')">Month</button>
        </div>
      </div>
      <div id="timeline-chart" class="timeline-chart">
        <div class="skeleton skeleton-chart"></div>
      </div>
    </div>
  </div>

  <div class="panel" id="domains">
    <div class="chart-container">
      <div class="chart-title">Top Domains</div>
      <div id="domains-chart" class="bar-chart">
        <div class="skeleton skeleton-chart"></div>
      </div>
    </div>
  </div>

  <div class="panel" id="clusters">
    <div class="chart-container">
      <div class="chart-title">Topic Clusters</div>
      <div id="clusters-chart" class="bubble-chart">
        <div class="skeleton skeleton-chart"></div>
      </div>
    </div>
  </div>

  <div class="panel" id="psh">
    <div id="psh-accordion" class="psh-accordion">
      <div class="skeleton skeleton-chart"></div>
    </div>
  </div>

  <div class="panel" id="sources">
    <div class="source-grid" id="sources-grid">
      <div class="stat skeleton skeleton-stat"></div>
      <div class="stat skeleton skeleton-stat"></div>
      <div class="stat skeleton skeleton-stat"></div>
      <div class="stat skeleton skeleton-stat"></div>
    </div>
  </div>
</div>

<script>
const $ = s => document.querySelector(s);
const $$ = s => document.querySelectorAll(s);

// --- URL routing (hash-based) ---
function updateHash(params) {
  const parts = Object.entries(params)
    .filter(([, v]) => v !== '' && v != null)
    .map(([k, v]) => k + '=' + encodeURIComponent(v));
  // Use location.hash directly — simpler, avoids replaceState quirks with tunneled origins.
  // Wrapping in replaceState to avoid pushing a history entry.
  const frag = parts.join('&');
  history.replaceState(null, '', frag ? '#' + frag : location.pathname + location.search);
}

function readHash() {
  const p = {};
  const raw = location.hash.replace(/^#/, '');
  if (!raw) return p;
  raw.split('&').forEach(part => {
    const eq = part.indexOf('=');
    if (eq > 0) {
      try { p[part.slice(0, eq)] = decodeURIComponent(part.slice(eq + 1)); }
      catch (_) { p[part.slice(0, eq)] = part.slice(eq + 1); }
    }
  });
  return p;
}

function activateTab(panel) {
  $$('.tab').forEach(x => x.classList.remove('active'));
  $$('.panel').forEach(x => x.classList.remove('active'));
  const tab = [...$$('.tab')].find(t => t.dataset.panel === panel);
  if (tab) tab.classList.add('active');
  const el = $('#' + panel);
  if (el) el.classList.add('active');
}

function restoreFromHash() {
  const p = readHash();

  // Source filter: hash wins over localStorage
  const src = p.source || localStorage.getItem('mp-source') || '';
  if (src) $('#source-filter').value = src;

  if (!p.tab) return;

  activateTab(p.tab);

  if (p.q) { $('#search-input').value = p.q; $('#search-clear').classList.add('visible'); $('#search-kbd').classList.add('hidden'); }

  switch (p.tab) {
    case 'results':  if (p.q || src) doSearch(); break;
    case 'timeline': loadTimeline(p.granularity || 'month'); break;
    case 'domains':  loadDomains(); break;
    case 'clusters': loadClusters(); break;
    case 'psh':      loadPSH(); break;
    case 'sources':  loadSources(); break;
  }
}

// --- Tabs ---
$$('.tab').forEach(t => t.addEventListener('click', () => {
  activateTab(t.dataset.panel);
  if (t.dataset.panel === 'timeline' && !timelineLoaded) loadTimeline('month');
  if (t.dataset.panel === 'domains' && !domainsLoaded) loadDomains();
  if (t.dataset.panel === 'clusters' && !clustersLoaded) loadClusters();
  if (t.dataset.panel === 'psh' && !pshLoaded) loadPSH();
  if (t.dataset.panel === 'sources' && !sourcesLoaded) loadSources();
  if (t.dataset.panel !== 'results') updateHash({tab: t.dataset.panel});
}));

// --- Keyboard shortcuts ---
document.addEventListener('keydown', e => {
  if (e.key === '/' && document.activeElement !== $('#search-input')) {
    e.preventDefault();
    $('#search-input').focus();
  }
  if (e.key === 'Escape') {
    if ($('#search-input').value) { clearSearch(); } else { $('#search-input').blur(); }
  }
});

$('#search-input').addEventListener('keydown', e => { if (e.key === 'Enter') doSearch(); });
$('#source-filter').addEventListener('change', function() { localStorage.setItem('mp-source', this.value); });
$('#search-input').addEventListener('input', () => {
  const hasVal = $('#search-input').value.length > 0;
  $('#search-clear').classList.toggle('visible', hasVal);
  $('#search-kbd').classList.toggle('hidden', hasVal);
});

function clearSearch() {
  $('#search-input').value = '';
  $('#search-clear').classList.remove('visible');
  $('#search-kbd').classList.remove('hidden');
  $('#search-input').focus();
  setResultsTabCount(null);
  updateHash({tab: 'results'});
  showEmpty();
}

function showEmpty() {
  $('#results').innerHTML = '<div class="empty-state"><div class="icon">&#x1f3db;</div>' +
    '<p>Search your personal memory or browse recent activity.</p></div>';
}

// --- API helpers ---
async function apiFetch(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error('API returned ' + res.status);
  return res.json();
}

function showError(el, msg, retryFn) {
  el.innerHTML = '<div class="error-state"><p>' + escHtml(msg) + '</p>' +
    (retryFn ? '<button onclick="' + retryFn + '">Retry</button>' : '') + '</div>';
}

// --- Search ---
function searchTag(el) {
  $('#search-input').value = el.dataset.tag;
  doSearch();
}

async function doSearch() {
  const q = $('#search-input').value.trim();
  const source = $('#source-filter').value;
  const params = new URLSearchParams();
  if (q) params.set('q', q);
  if (source) params.set('source', source);
  params.set('limit', '100');

  updateHash({tab: 'results', q: q || undefined, source: source || undefined});

  const el = $('#results');
  el.innerHTML = Array(5).fill('<div class="skeleton skeleton-result"></div>').join('');
  activateTab('results');

  try {
    const data = await apiFetch('/api/search?' + params);
    renderResults(data || [], q);
  } catch (e) {
    showError(el, 'Search failed: ' + e.message, 'doSearch()');
  }
}

function setResultsTabCount(n) {
  const t = [...$$('.tab')].find(x => x.dataset.panel === 'results');
  if (t) t.textContent = n != null ? 'Browse (' + n + ')' : 'Browse';
}

function renderResults(results, query) {
  const el = $('#results');
  if (!results.length) {
    setResultsTabCount(0);
    el.innerHTML = '<div class="empty-state"><div class="icon">&#x1f50d;</div>' +
      '<p>No results found' + (query ? ' for "' + escHtml(query) + '"' : '') + '</p></div>';
    return;
  }
  setResultsTabCount(results.length);

  const countHtml = '<div class="result-count">' + results.length + ' result' + (results.length !== 1 ? 's' : '') +
    (query ? ' for "' + escHtml(query) + '"' : '') + '</div>';

  el.innerHTML = countHtml + '<div class="results">' + results.map(r => {
    const titleText = r.title || r.url || '(untitled)';
    const titleContent = r.url
      ? buildLink(r.url, highlight(titleText, query))
      : highlight(titleText, query);

    let html = '<div class="result">' +
      '<div class="result-header">' +
        '<span class="result-source src-' + r.source + '" onclick="filterSource(\'' + r.source + '\')" style="cursor:pointer" title="Browse ' + formatSource(r.source) + '">' + formatSource(r.source) + '</span>' +
        '<span class="result-time" title="' + r.time + '">' + relativeTime(r.unix) + '</span>' +
      '</div>' +
      '<div class="result-title">' + titleContent + '</div>';

    if (r.location) html += '<div class="result-location">' + escHtml(r.location) + '</div>';
    if (r.body && r.body.length > 2) html += '<div class="result-body">' + highlight(truncate(r.body, 200), query) + '</div>';
    if (r.summary) html += '<div class="result-summary">' + escHtml(truncate(r.summary, 300)) + '</div>';
    if (r.psh_tags && r.psh_tags.length) {
      html += '<div class="psh-tags">' + r.psh_tags.map(t =>
        '<span class="psh-tag" data-tag="' + escHtml(t) + '" onclick="searchTag(this)">' + escHtml(t) + '</span>'
      ).join('') + '</div>';
    }
    html += '</div>';
    return html;
  }).join('') + '</div>';
}

function formatSource(s) {
  return s.replace('safari_', '').replace('_', ' ');
}

function highlight(text, query) {
  if (!query || query.length < 2) return escHtml(text);
  const safe = escHtml(text);
  const words = query.split(/\s+/).filter(w => w.length > 1).map(w => escRegex(w));
  if (!words.length) return safe;
  const re = new RegExp('(' + words.join('|') + ')', 'gi');
  return safe.replace(re, '<mark style="background:rgba(88,166,255,0.2);color:var(--accent);border-radius:2px;padding:0 1px">$1</mark>');
}

// --- Shared source colours (all 10 sources) ---
const SRC_COLORS = {
  safari_history: '#58a6ff', safari_bookmarks: '#3fb950',
  safari_open_tabs: '#79c0ff', safari_icloud_tabs: '#56d364',
  safari_reading_list: '#388bfd', calendar: '#d29922',
  reminders: '#bc8cff', notes: '#f778ba', zotero: '#cc3333', archivebox: '#f0883e',
  knowledgec: '#a371f7'
};
const SRC_LABELS = {
  safari_history: 'Safari History', safari_bookmarks: 'Bookmarks',
  safari_open_tabs: 'Open Tabs', safari_icloud_tabs: 'iCloud Tabs',
  safari_reading_list: 'Reading List', calendar: 'Calendar',
  reminders: 'Reminders', notes: 'Notes', zotero: 'Zotero', archivebox: 'ArchiveBox',
  knowledgec: 'App Usage'
};
function formatSourceFull(s) { return SRC_LABELS[s] || s.replace(/_/g, ' '); }

function filterSource(source) {
  $('#source-filter').value = source;
  doSearch();
}
function switchToSources() {
  const tab = [...$$('.tab')].find(t => t.dataset.panel === 'sources');
  if (tab) tab.click();
}

// --- Stats ---
async function loadStats() {
  try {
    const s = await apiFetch('/api/stats');
    const bar = $('#stats-bar');
    bar.innerHTML =
      stat(fmtNum(s.total), 'Total Records') +
      stat(Object.keys(s.by_source).length, 'Sources', 'switchToSources()') +
      stat(fmtNum(s.enriched), 'Enriched') +
      stat(s.oldest, 'Oldest') +
      stat(s.newest, 'Newest');
    $('#header-meta').textContent = fmtNum(s.total) + ' memories indexed';
  } catch (e) {
    $('#stats-bar').innerHTML = '<div class="error-state"><p>Failed to load stats</p>' +
      '<button onclick="loadStats()">Retry</button></div>';
  }
}
function stat(val, label, onclick) {
  const attrs = onclick ? ' onclick="' + onclick + '" style="cursor:pointer" title="Click to browse"' : '';
  return '<div class="stat"' + attrs + '><div class="stat-value">' + val + '</div><div class="stat-label">' + label + '</div></div>';
}

// --- Sources ---
let sourcesLoaded = false;
async function loadSources() {
  const el = $('#sources-grid');
  sourcesLoaded = true;
  try {
    const s = await apiFetch('/api/stats');
    const entries = Object.entries(s.by_source).sort((a, b) => b[1] - a[1]);
    el.innerHTML = entries.map(([src, count]) => {
      const color = SRC_COLORS[src] || '#8b949e';
      const label = formatSourceFull(src);
      return '<div class="source-card" onclick="filterSource(\'' + src + '\')" title="Browse ' + label + '">' +
        '<div class="source-card-value" style="color:' + color + '">' + fmtNum(count) + '</div>' +
        '<div class="source-card-label">' + label + '</div>' +
        '<div class="source-card-hint">Click to browse</div>' +
      '</div>';
    }).join('');
  } catch (e) {
    showError(el, 'Failed to load sources: ' + e.message, 'loadSources()');
  }
}

// --- Timeline ---
let timelineLoaded = false;
let currentGranularity = 'month';

async function loadTimeline(granularity) {
  granularity = granularity || 'month';
  currentGranularity = granularity;
  timelineLoaded = true;
  updateHash({tab: 'timeline', granularity: granularity !== 'month' ? granularity : undefined});

  // Update button states
  $$('.chart-btn').forEach(b => b.classList.toggle('active', b.textContent.toLowerCase() === granularity));

  const el = $('#timeline-chart');
  el.innerHTML = '<div class="skeleton skeleton-chart"></div>';

  try {
    const data = await apiFetch('/api/timeline?granularity=' + granularity);
    if (!data || !data.length) { el.innerHTML = '<div class="empty-state"><p>No timeline data</p></div>'; return; }

    const byPeriod = {};
    const sources = new Set();
    data.forEach(d => {
      if (!byPeriod[d.period]) byPeriod[d.period] = {};
      byPeriod[d.period][d.source] = (byPeriod[d.period][d.source] || 0) + d.count;
      sources.add(d.source);
    });

    const periods = Object.keys(byPeriod).sort();
    const maxVal = Math.max(...periods.map(p => Object.values(byPeriod[p]).reduce((a,b) => a+b, 0)));

    const colors = SRC_COLORS;

    const barWidth = Math.max(3, Math.min(18, 800 / periods.length));
    const gap = Math.max(1, Math.min(3, barWidth / 4));
    const svgWidth = periods.length * (barWidth + gap) + 60;
    const svgHeight = 180;
    const chartBottom = svgHeight - 24;

    let svg = '<svg width="' + svgWidth + '" height="' + svgHeight + '" style="min-width:100%">';

    // Grid lines
    for (let i = 0; i <= 4; i++) {
      const y = 10 + (chartBottom - 10) * (1 - i/4);
      svg += '<line x1="20" y1="' + y + '" x2="' + (svgWidth-10) + '" y2="' + y + '" stroke="#21262d" stroke-width="1"/>';
      svg += '<text x="16" y="' + (y+3) + '" fill="#484f58" font-size="8" text-anchor="end">' + Math.round(maxVal * i/4) + '</text>';
    }

    periods.forEach((p, i) => {
      const x = i * (barWidth + gap) + 30;
      let y = chartBottom;
      [...sources].forEach(src => {
        const val = byPeriod[p][src] || 0;
        const h = (val / maxVal) * (chartBottom - 10);
        y -= h;
        if (h > 0.5) {
          svg += '<rect x="' + x + '" y="' + y + '" width="' + barWidth + '" height="' + h +
            '" fill="' + (colors[src]||'#666') + '" opacity="0.85" rx="1">' +
            '<title>' + p + ' / ' + formatSource(src) + ': ' + val + '</title></rect>';
        }
      });
      const labelEvery = Math.max(1, Math.floor(periods.length / 14));
      if (i % labelEvery === 0) {
        svg += '<text x="' + (x + barWidth/2) + '" y="' + (svgHeight - 6) + '" fill="#484f58" font-size="8" text-anchor="middle">' + p + '</text>';
      }
    });
    svg += '</svg>';

    let legend = '<div class="timeline-legend">';
    [...sources].forEach(src => {
      legend += '<span class="timeline-legend-item">' +
        '<span class="timeline-legend-dot" style="background:' + (colors[src]||'#666') + '"></span>' +
        formatSource(src) + '</span>';
    });
    legend += '</div>';

    el.innerHTML = svg + legend;
  } catch (e) {
    showError(el, 'Failed to load timeline: ' + e.message, "loadTimeline('" + granularity + "')");
  }
}

// --- Domains ---
let domainsLoaded = false;
async function loadDomains() {
  const el = $('#domains-chart');
  el.innerHTML = '<div class="skeleton skeleton-chart"></div>';
  domainsLoaded = true;

  try {
    const data = await apiFetch('/api/domains?limit=25');
    if (!data || !data.length) { el.innerHTML = '<div class="empty-state"><p>No domain data</p></div>'; return; }

    const max = data[0].count;
    const colors = ['#58a6ff','#3fb950','#d29922','#bc8cff','#f778ba','#f85149','#79c0ff','#7ee787'];
    el.innerHTML = data.map((d, i) => {
      const pct = (d.count / max * 100).toFixed(1);
      const color = colors[i % colors.length];
      return '<div class="bar-row" onclick="$(\'#search-input\').value=\'' + escHtml(d.name) + '\';doSearch()" title="Search ' + escHtml(d.name) + '">' +
        '<span class="bar-label" title="' + escHtml(d.name) + '">' + escHtml(d.name) + '</span>' +
        '<div class="bar-track"><div class="bar" style="width:' + pct + '%;background:' + color + '">' +
        (pct > 15 ? '<span class="bar-inner-label">' + fmtNum(d.count) + '</span>' : '') +
        '</div></div>' +
        '<span class="bar-value">' + fmtNum(d.count) + '</span></div>';
    }).join('');
  } catch (e) {
    showError(el, 'Failed to load domains: ' + e.message, 'loadDomains()');
  }
}

// --- Clusters ---
let clustersLoaded = false;
async function loadClusters() {
  const el = $('#clusters-chart');
  el.innerHTML = '<div class="skeleton skeleton-chart"></div>';
  clustersLoaded = true;

  try {
    const data = await apiFetch('/api/clusters');
    if (!data || !data.length) { el.innerHTML = '<div class="empty-state"><p>No topic data</p></div>'; return; }

    const max = data[0].count;
    const srcColors = SRC_COLORS;

    el.innerHTML = data.map(c => {
      const mainSrc = (c.sources && c.sources[0]) || 'safari_history';
      const color = srcColors[mainSrc] || '#58a6ff';
      const scale = 0.6 + (c.count / max) * 0.4;
      return '<div class="bubble" style="font-size:' + (scale * 0.85).toFixed(2) + 'rem" ' +
        'onclick="$(\'#search-input\').value=\'' + escHtml(c.topic) + '\';doSearch()">' +
        '<span class="dot" style="background:' + color + '"></span>' +
        escHtml(c.topic) +
        '<span class="count">' + fmtNum(c.count) + '</span></div>';
    }).join('');
  } catch (e) {
    showError(el, 'Failed to load topics: ' + e.message, 'loadClusters()');
  }
}

// --- PSH Navigator ---
let pshLoaded = false;
let pshData = [];

async function loadPSH() {
  const el = $('#psh-accordion');
  el.innerHTML = '<div class="skeleton skeleton-chart"></div>';
  pshLoaded = true;

  try {
    pshData = await apiFetch('/api/psh');
    if (!pshData || !pshData.length) {
      el.innerHTML = '<div class="empty-state"><div class="icon">&#x1f4da;</div><p>No PSH classification data available.</p></div>';
      return;
    }
    renderPSHAccordion(el);
  } catch (e) {
    showError(el, 'Failed to load PSH data: ' + e.message, 'loadPSH()');
  }
}

function renderPSHAccordion(el) {
  el.innerHTML = pshData.map((sec, si) => {
    const hasL2 = sec.subs && sec.subs.length > 0;
    return '<div class="psh-section" id="psh-sec-' + si + '">' +
      '<div class="psh-section-header" onclick="togglePSHSection(' + si + ')">' +
        '<span class="psh-chevron">&#x25B6;</span>' +
        '<span class="psh-section-name">' + escHtml(sec.l1) + '</span>' +
        '<span class="psh-section-count">' + fmtNum(sec.total) + ' items</span>' +
      '</div>' +
      '<div class="psh-section-body" id="psh-body-' + si + '" style="display:none">' +
        (hasL2 ? renderPSHBars(sec, si) : '<div class="psh-items-header">No sub-categories</div>') +
        '<div class="psh-items" id="psh-items-' + si + '"></div>' +
      '</div>' +
    '</div>';
  }).join('');
}

function renderPSHBars(sec, si) {
  const max = sec.subs[0] ? sec.subs[0].count : 1;
  const bars = sec.subs.map((sub, li) => {
    const pct = (sub.count / max * 100).toFixed(1);
    return '<div class="psh-bar-row" id="psh-bar-' + si + '-' + li + '" ' +
      'onclick="selectPSHBar(' + si + ',' + li + ',\'' + escHtml(sec.l1) + '\',\'' + escHtml(sub.l2 || '') + '\')">' +
      '<span class="psh-bar-label" title="' + escHtml(sub.l2 || '(unclassified)') + '">' + escHtml(sub.l2 || '(unclassified)') + '</span>' +
      '<div class="psh-bar-track"><div class="psh-bar-fill" style="width:' + pct + '%"></div></div>' +
      '<span class="psh-bar-count">' + fmtNum(sub.count) + '</span>' +
    '</div>';
  }).join('');
  return '<div class="psh-bars">' + bars + '</div>';
}

function togglePSHSection(si) {
  const sec = $('#psh-sec-' + si);
  const body = $('#psh-body-' + si);
  const open = sec.classList.toggle('open');
  body.style.display = open ? '' : 'none';
}

let pshActiveBar = null;

async function selectPSHBar(si, li, l1, l2) {
  const barId = si + '-' + li;
  const itemsEl = $('#psh-items-' + si);

  // Deselect previously selected bar (same or different section)
  if (pshActiveBar) {
    const prev = $('#psh-bar-' + pshActiveBar);
    if (prev) prev.classList.remove('selected');
    // If toggling the same bar, close it
    if (pshActiveBar === barId) {
      pshActiveBar = null;
      itemsEl.innerHTML = '';
      return;
    }
    // If different section, clear that section's items
    const prevSi = pshActiveBar.split('-')[0];
    if (prevSi !== String(si)) {
      const prevItems = $('#psh-items-' + prevSi);
      if (prevItems) prevItems.innerHTML = '';
    }
  }

  pshActiveBar = barId;
  $('#psh-bar-' + barId).classList.add('selected');

  itemsEl.innerHTML = '<div class="psh-items-header">Loading...</div>';
  await fetchPSHItems(l1, l2, 0, 20, itemsEl, false);
}

async function fetchPSHItems(l1, l2, offset, limit, el, append) {
  try {
    const params = new URLSearchParams({ l1, offset, limit });
    if (l2) params.set('l2', l2);
    const data = await apiFetch('/api/psh/items?' + params);
    if (!data || !data.items) { el.innerHTML = '<div class="psh-items-header">No items found.</div>'; return; }

    const header = '<div class="psh-items-header">' + fmtNum(data.total) + ' items' +
      (l2 ? ' in <strong>' + escHtml(l2) + '</strong>' : ' in <strong>' + escHtml(l1) + '</strong>') + '</div>';

    const cards = '<div class="results">' + data.items.map(r => {
      const titleText = r.title || r.url || '(untitled)';
      const titleContent = r.url
        ? buildLink(r.url, escHtml(titleText))
        : escHtml(titleText);
      let html = '<div class="result">' +
        '<div class="result-header">' +
          '<span class="result-source src-' + (r.source||'zotero') + '">' + formatSource(r.source||'zotero') + '</span>' +
          '<span class="result-time">' + relativeTime(r.timestamp) + '</span>' +
        '</div>' +
        '<div class="result-title">' + titleContent + '</div>';
      if (r.psh_tags && r.psh_tags.length) {
        html += '<div class="psh-tags">' + r.psh_tags.map(t =>
          '<span class="psh-tag" data-tag="' + escHtml(t) + '" onclick="searchTag(this)">' + escHtml(t) + '</span>'
        ).join('') + '</div>';
      }
      html += '</div>';
      return html;
    }).join('') + '</div>';

    const nextOffset = offset + data.items.length;
    const moreBtn = nextOffset < data.total
      ? '<button class="psh-load-more" onclick="loadMorePSHItems(\'' + escHtml(l1) + '\',\'' + escHtml(l2||'') + '\',' + nextOffset + ',this.parentElement)">Load more</button>'
      : '';

    if (append) {
      // Replace load-more button with new cards + new button
      const btn = el.querySelector('.psh-load-more');
      if (btn) btn.remove();
      el.insertAdjacentHTML('beforeend', cards + moreBtn);
    } else {
      el.innerHTML = header + cards + moreBtn;
    }
  } catch(e) {
    el.innerHTML = '<div class="psh-items-header" style="color:var(--red)">Failed: ' + escHtml(e.message) + '</div>';
  }
}

async function loadMorePSHItems(l1, l2, offset, el) {
  await fetchPSHItems(l1, l2, offset, 20, el, true);
}

// --- Helpers ---
// True on macOS desktop Safari/Chrome; false on iPhone/iPad.
const IS_MAC = /Macintosh/.test(navigator.userAgent) && !/iPhone|iPad/.test(navigator.userAgent);

function buildLink(url, labelHtml) {
  if (!url) return labelHtml;
  if (!url.startsWith('http://') && !url.startsWith('https://')) {
    // notes:// and similar custom schemes only work on macOS.
    // On iOS the scheme doesn't exist and Safari rejects it.
    if (IS_MAC) {
      return '<a href="' + escHtml(url) + '">' + labelHtml + '</a>';
    }
    return labelHtml + '<span style="font-size:0.6rem;color:var(--text-faint);margin-left:0.3rem" title="Deep link — open on Mac">macOS</span>';
  }
  return '<a href="' + escHtml(url) + '" target="_blank" rel="noopener">' + labelHtml + '</a>';
}

function escHtml(s) {
  if (!s) return '';
  const d = document.createElement('div');
  d.textContent = s;
  return d.innerHTML;
}
function escRegex(s) { return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }
function truncate(s, n) { return s && s.length > n ? s.substring(0, n) + '...' : s || ''; }
function fmtNum(n) { return Number(n).toLocaleString(); }

function relativeTime(unix) {
  if (!unix) return '';
  const now = Date.now() / 1000;
  const diff = now - unix;
  if (diff < 0) return new Date(unix * 1000).toLocaleDateString();
  if (diff < 60) return 'just now';
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
  if (diff < 604800) return Math.floor(diff / 86400) + 'd ago';
  if (diff < 2592000) return Math.floor(diff / 604800) + 'w ago';
  return new Date(unix * 1000).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
}

// --- Init ---
loadStats();
// Defer one tick so the DOM is fully painted before routing kicks in.
setTimeout(restoreFromHash, 0);
</script>
</body>
</html>`

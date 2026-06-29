/**
 * app.js — dsize Web UI application logic.
 * Handles: EventSource wiring, DOM updates, view transitions,
 * client-side sort, tooltips, dark-mode, history fetch/render.
 */
(function () {
  'use strict';

  // ── State ──────────────────────────────────────────────────────────────
  var allEntries = [];   // complete payload entries
  var sortKey    = 'size';
  var sortAsc    = false;
  var scanTarget = '';

  // ── DOM refs ────────────────────────────────────────────────────────────
  var progressView   = document.getElementById('progress-view');
  var resultsView    = document.getElementById('results-view');
  var historyView    = document.getElementById('history-view');
  var barList        = document.getElementById('bar-list');
  var tooltip        = document.getElementById('tooltip');
  var currentPathEl  = document.getElementById('current-path');
  var statTotal      = document.getElementById('stat-total');
  var statFiles      = document.getElementById('stat-files');
  var statDirs       = document.getElementById('stat-dirs');
  var statElapsed    = document.getElementById('stat-elapsed');
  var statFps        = document.getElementById('stat-fps');
  var resultsSummary = document.getElementById('results-summary');
  var totalHeroEl    = document.getElementById('total-hero-value');
  var historyCanvas  = document.getElementById('history-chart');
  var historyTbody   = document.getElementById('history-tbody');
  var breadcrumbEl   = document.getElementById('breadcrumb');

  // ── Dark mode (FR-6) ────────────────────────────────────────────────────
  (function initTheme() {
    var saved = localStorage.getItem('colorScheme');
    if (saved === 'dark') document.documentElement.classList.add('dark');
    else if (saved === 'light') document.documentElement.classList.add('light');
  })();

  document.getElementById('theme-toggle').addEventListener('click', function () {
    var isDark = document.documentElement.classList.toggle('dark');
    document.documentElement.classList.toggle('light', !isDark);
    localStorage.setItem('colorScheme', isDark ? 'dark' : 'light');
  });

  // ── Tabs ─────────────────────────────────────────────────────────────────
  document.querySelectorAll('.tab-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var target = btn.dataset.tab;
      document.querySelectorAll('.tab-btn').forEach(function (b) { b.classList.remove('active'); });
      btn.classList.add('active');
      resultsView.style.display  = (target === 'results')  ? 'block' : 'none';
      historyView.style.display  = (target === 'history')  ? 'block' : 'none';
      if (target === 'history') loadHistory();
    });
  });

  // ── Folder selection ─────────────────────────────────────────────────────
  // Open the native OS folder dialog (server-side); on success the server sets
  // it as the scan root and we reload to scan it.
  function pickFolder() {
    fetch('/pick', { method: 'POST' })
      .then(function (r) {
        if (r.status === 204) return;            // cancelled
        if (r.status === 501) { showPathFallback(); toast('No native dialog available — enter a path below.'); return; }
        if (!r.ok) return r.text().then(function (t) { throw new Error(t.trim() || ('HTTP ' + r.status)); });
        window.location.href = '/';              // scan the newly chosen root
      })
      .catch(function (err) { toast('Could not open folder dialog: ' + err.message); });
  }

  var pickBtn = document.getElementById('pick-btn');
  if (pickBtn) pickBtn.addEventListener('click', pickFolder);
  var welcomePickBtn = document.getElementById('welcome-pick-btn');
  if (welcomePickBtn) welcomePickBtn.addEventListener('click', pickFolder);

  function showPathFallback() {
    var f = document.getElementById('path-fallback');
    if (f) f.style.display = 'block';
  }
  var pathForm = document.getElementById('path-fallback');
  if (pathForm) {
    pathForm.addEventListener('submit', function (ev) {
      ev.preventDefault();
      var v = document.getElementById('path-input').value.trim();
      if (!v) return;
      fetch('/pick?path=' + encodeURIComponent(v), { method: 'POST' })
        .then(function (r) {
          if (!r.ok) return r.text().then(function (t) { throw new Error(t.trim() || ('HTTP ' + r.status)); });
          window.location.href = '/';
        })
        .catch(function (err) { toast(err.message); });
    });
  }

  // ── SSE / EventSource ──────────────────────────────────────────────────
  var es;

  // startScan opens the SSE stream for the current (or ?root=) directory.
  function startScan() {
    hideWelcome();
    progressView.style.display = '';
    // A ?root= query (breadcrumb / "scan this directory") re-roots the scan.
    var rootParam = new URLSearchParams(window.location.search).get('root');
    es = new EventSource('/events' + (rootParam ? '?root=' + encodeURIComponent(rootParam) : ''));

    es.addEventListener('progress', function (ev) {
      updateProgress(JSON.parse(ev.data));
    });

    es.addEventListener('complete', function (ev) {
      var d = JSON.parse(ev.data);
      allEntries = d.entries || [];
      scanTarget = d.target || '';
      es.close();
      showResults(d);
      document.dispatchEvent(new CustomEvent('dsize:complete', { detail: d }));
    });

    es.addEventListener('error', function (ev) {
      if (ev.data) {
        var d = JSON.parse(ev.data);
        currentPathEl.textContent = 'Error: ' + d.message;
      }
    });
  }

  // ── Welcome / empty state ────────────────────────────────────────────────
  var welcomeEl = document.getElementById('welcome');
  function showWelcome() {
    if (welcomeEl) welcomeEl.style.display = 'block';
    var sub = document.getElementById('header-subtitle');
    if (sub) sub.textContent = 'No folder selected';
    progressView.style.display = 'none';
    resultsView.style.display = 'none';
    historyView.style.display = 'none';
    var tabs = document.getElementById('tabs');
    if (tabs) tabs.style.display = 'none';
    if (breadcrumbEl) breadcrumbEl.style.display = 'none';
  }
  function hideWelcome() {
    if (welcomeEl) welcomeEl.style.display = 'none';
  }

  // On load, ask the server whether a folder is selected: scan it, else welcome.
  fetch('/state')
    .then(function (r) { return r.json(); })
    .then(function (s) {
      var hasRoot = s && s.root;
      var rootParam = new URLSearchParams(window.location.search).get('root');
      if (hasRoot || rootParam) startScan();
      else showWelcome();
    })
    .catch(function () { startScan(); }); // fail open: try scanning

  // ── Progress updates (FR-4) ────────────────────────────────────────────
  function updateProgress(d) {
    if (currentPathEl) currentPathEl.textContent = d.currentPath || '';
    if (statTotal)   statTotal.textContent   = fmtBytes(d.totalSize || 0);
    if (statFiles)   statFiles.textContent   = fmt(d.fileCount || 0);
    if (statDirs)    statDirs.textContent    = fmt(d.dirCount  || 0);
    if (statElapsed) statElapsed.textContent = fmtElapsed(d.elapsedMs || 0);
    if (statFps)     statFps.textContent     = (d.filesPerSec || 0).toFixed(0) + '/s';
  }

  // ── Results view (FR-5) ────────────────────────────────────────────────
  function showResults(d) {
    progressView.style.display = 'none';
    resultsView.style.display  = 'block';

    // Activate results tab.
    document.querySelectorAll('.tab-btn').forEach(function (b) { b.classList.remove('active'); });
    var resTab = document.querySelector('.tab-btn[data-tab="results"]');
    if (resTab) resTab.classList.add('active');

    if (totalHeroEl) totalHeroEl.textContent = fmtBytes(d.totalSize);
    if (resultsSummary) {
      resultsSummary.textContent =
        plural(d.fileCount, 'file') + ' · ' +
        plural(d.dirCount, 'dir') + ' · scanned in ' +
        fmtElapsed(d.elapsedMs);
    }

    scanTarget = d.target || '';
    renderBreadcrumb(d.base || d.target || '', d.target || '');
    updateSortIndicators();
    renderBars();
  }

  function plural(n, word) {
    return fmt(n) + ' ' + word + (n === 1 ? '' : 's');
  }

  // renderBreadcrumb shows the path from the startup root (base) down to the
  // current scan root, each ancestor clickable to re-scan at that level. The
  // base is the floor — you cannot navigate above it.
  function renderBreadcrumb(base, current) {
    if (!breadcrumbEl) return;
    base = stripSlash(base);
    current = stripSlash(current);

    var crumbs = [{ label: base || '/', path: base }];
    if (current && current !== base && current.indexOf(base + '/') === 0) {
      var acc = base;
      current.slice(base.length + 1).split('/').forEach(function (seg) {
        acc = acc + '/' + seg;
        crumbs.push({ label: seg, path: acc });
      });
    }

    breadcrumbEl.innerHTML = '';
    var icon = document.createElement('span');
    icon.className = 'breadcrumb-icon';
    icon.textContent = '📁';
    breadcrumbEl.appendChild(icon);
    crumbs.forEach(function (c, i) {
      if (i > 0) {
        var sep = document.createElement('span');
        sep.className = 'crumb-sep';
        sep.textContent = '›';
        breadcrumbEl.appendChild(sep);
      }
      var isCurrent = i === crumbs.length - 1;
      if (isCurrent) {
        var cur = document.createElement('span');
        cur.className = 'crumb crumb-current';
        cur.textContent = c.label;
        breadcrumbEl.appendChild(cur);
      } else {
        var a = document.createElement('button');
        a.type = 'button';
        a.className = 'crumb crumb-link';
        a.textContent = c.label;
        a.title = c.path;
        a.addEventListener('click', function () { scanRoot(c.path); });
        breadcrumbEl.appendChild(a);
      }
    });
    breadcrumbEl.style.display = 'flex';
  }

  function stripSlash(p) {
    return p && p.length > 1 ? p.replace(/\/+$/, '') : p;
  }

  function renderBars() {
    var entries = allEntries.slice();
    entries.sort(function (a, b) {
      var v;
      if      (sortKey === 'size')  v = a.size - b.size;
      else if (sortKey === 'name')  v = a.path.localeCompare(b.path);
      else if (sortKey === 'trend') v = trendRank(a.trend) - trendRank(b.trend);
      else v = 0;
      return sortAsc ? v : -v;
    });

    var maxSize = entries.reduce(function (m, e) { return Math.max(m, e.size); }, 1);
    barList.innerHTML = '';

    entries.forEach(function (e) {
      var pct = Math.max(0.004, e.size / maxSize) * 100; // min 0.4% ≈ 4px on 1000px
      var row  = document.createElement('div');
      row.className = 'bar-row ' + (e.isDir ? 'is-dir' : 'is-file');

      var fill = document.createElement('div');
      fill.className = 'bar-fill';
      fill.style.width = pct + '%';

      var name = document.createElement('div');
      name.className = 'bar-name';
      name.textContent = basename(e.path);

      var size = document.createElement('div');
      size.className = 'bar-size';
      size.textContent = fmtBytes(e.size);

      var trend = document.createElement('div');
      trend.className = 'bar-trend ' + trendClass(e.trend);
      trend.textContent = trendIcon(e.trend);

      // Per-row actions, revealed on hover (see style.css).
      var actions = document.createElement('div');
      actions.className = 'bar-actions';
      if (e.isDir) {
        actions.appendChild(makeActionBtn('🔍', 'Scan', 'Scan this folder', function () { scanRoot(e.path); }));
        actions.appendChild(makeActionBtn('📂', 'Open', 'Open folder in file manager', function () { openPath(e.path, false); }));
      } else {
        actions.appendChild(makeActionBtn('📄', 'Reveal', 'Reveal file in its folder', function () { openPath(e.path, true); }));
      }

      row.appendChild(fill);
      row.appendChild(name);
      row.appendChild(size);
      row.appendChild(trend);
      row.appendChild(actions);

      // Tooltip (FR-5.6)
      row.addEventListener('mouseenter', function (ev) {
        showTooltip(ev, e, maxSize);
      });
      row.addEventListener('mousemove', function (ev) {
        positionTooltip(ev);
      });
      row.addEventListener('mouseleave', function () {
        tooltip.classList.remove('visible');
      });

      barList.appendChild(row);
    });
  }

  // ── Row actions ──────────────────────────────────────────────────────────
  function makeActionBtn(icon, label, title, onClick) {
    var b = document.createElement('button');
    b.className = 'bar-action-btn';
    b.type = 'button';
    b.title = title;
    b.setAttribute('aria-label', title);
    b.innerHTML = '<span class="bar-action-icon">' + icon + '</span>' +
                  '<span class="bar-action-label"></span>';
    b.querySelector('.bar-action-label').textContent = label;
    b.addEventListener('click', function (ev) {
      ev.stopPropagation();
      onClick();
    });
    return b;
  }

  // openPath asks the server to open a directory (reveal=false) or reveal a
  // file in its folder (reveal=true) in the OS file manager.
  function openPath(path, reveal) {
    fetch('/open?path=' + encodeURIComponent(path) + (reveal ? '&reveal=1' : ''), { method: 'POST' })
      .then(function (r) {
        if (!r.ok) return r.text().then(function (t) { throw new Error(t.trim() || ('HTTP ' + r.status)); });
      })
      .catch(function (err) { toast('Could not open: ' + err.message); });
  }

  // scanRoot reloads the UI rooted at path, which re-runs the scan there.
  function scanRoot(path) {
    window.location.href = '/?root=' + encodeURIComponent(path);
  }

  var toastTimer;
  function toast(msg) {
    var t = document.getElementById('toast');
    if (!t) {
      t = document.createElement('div');
      t.id = 'toast';
      document.body.appendChild(t);
    }
    t.textContent = msg;
    t.classList.add('visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { t.classList.remove('visible'); }, 3500);
  }

  // ── Tooltip ──────────────────────────────────────────────────────────────
  function showTooltip(ev, entry, maxSize) {
    var pct = maxSize > 0 ? ((entry.size / maxSize) * 100).toFixed(1) : '0.0';
    document.getElementById('tt-path').textContent  = entry.path;
    document.getElementById('tt-bytes').textContent =
      fmt(entry.size) + ' bytes (' + pct + '% of largest)';
    tooltip.classList.add('visible');
    positionTooltip(ev);
  }

  function positionTooltip(ev) {
    var x = ev.clientX + 14;
    var y = ev.clientY + 14;
    if (x + 300 > window.innerWidth)  x = ev.clientX - 320;
    if (y + 80  > window.innerHeight) y = ev.clientY - 80;
    tooltip.style.left = x + 'px';
    tooltip.style.top  = y + 'px';
  }

  // ── Sort buttons (FR-5.5) ─────────────────────────────────────────────
  var SORT_LABELS = { size: 'Size', name: 'Name', trend: 'Trend' };
  function updateSortIndicators() {
    document.querySelectorAll('.sort-btn').forEach(function (b) {
      var active = b.dataset.sort === sortKey;
      b.classList.toggle('active', active);
      b.textContent = SORT_LABELS[b.dataset.sort] + (active ? (sortAsc ? ' ↑' : ' ↓') : '');
    });
  }
  document.querySelectorAll('.sort-btn').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var key = btn.dataset.sort;
      if (sortKey === key) { sortAsc = !sortAsc; }
      else { sortKey = key; sortAsc = key === 'name'; }
      updateSortIndicators();
      renderBars();
    });
  });

  // ── History (FR-8) ───────────────────────────────────────────────────
  function loadHistory() {
    if (!scanTarget) return;
    fetch('/history?target=' + encodeURIComponent(scanTarget))
      .then(function (r) { return r.json(); })
      .then(function (summaries) {
        renderHistoryTable(summaries);
        if (window.dsizeCharts && historyCanvas) {
          dsizeCharts.renderHistoryChart(historyCanvas, summaries);
        }
      })
      .catch(function (err) {
        console.error('history fetch error:', err);
      });
  }

  function renderHistoryTable(summaries) {
    if (!historyTbody) return;
    // Display descending by date (FR-8.3).
    var rows = summaries.slice().reverse();
    historyTbody.innerHTML = '';
    rows.forEach(function (s, i) {
      var prev = rows[i + 1]; // next in reversed = older
      var delta = prev ? s.totalSize - prev.totalSize : null;
      var tr = document.createElement('tr');
      tr.innerHTML =
        '<td>' + (s.scannedAt || '').replace('T', ' ').slice(0, 19) + '</td>' +
        '<td>' + fmtBytes(s.totalSize) + '</td>' +
        '<td class="' + (delta > 0 ? 'delta-pos' : delta < 0 ? 'delta-neg' : '') + '">' +
          (delta === null ? '—' : (delta > 0 ? '+' : '') + fmtBytes(delta)) +
        '</td>' +
        '<td>' + fmt(s.fileCount) + '</td>' +
        '<td>' + fmtElapsed(s.durationMs) + '</td>';
      historyTbody.appendChild(tr);
    });
  }

  // ── Helpers ───────────────────────────────────────────────────────────
  function fmtBytes(b) {
    b = b || 0;
    if (b >= 1099511627776) return (b / 1099511627776).toFixed(1) + ' TiB';
    if (b >= 1073741824)    return (b / 1073741824).toFixed(1) + ' GiB';
    if (b >= 1048576)       return (b / 1048576).toFixed(1) + ' MiB';
    if (b >= 1024)          return (b / 1024).toFixed(1) + ' KiB';
    return b + ' B';
  }

  function fmt(n) { return (n || 0).toLocaleString(); }

  function fmtElapsed(ms) {
    var s = Math.floor((ms || 0) / 1000);
    var h = Math.floor(s / 3600);
    var m = Math.floor((s % 3600) / 60);
    var sec = s % 60;
    return pad(h) + ':' + pad(m) + ':' + pad(sec);
  }

  function pad(n) { return n < 10 ? '0' + n : '' + n; }

  function basename(p) {
    return p.replace(/\\/g, '/').replace(/\/$/, '').split('/').pop() || p;
  }

  function trendIcon(t) {
    return { up: '▲', down: '▼', equal: '=', new: '★' }[t] || '';
  }
  function trendClass(t) {
    return { up: 'trend-up', down: 'trend-down', equal: 'trend-equal', new: 'trend-new' }[t] || '';
  }
  function trendRank(t) {
    return { up: 0, new: 1, equal: 2, down: 3 }[t] || 4;
  }

})();

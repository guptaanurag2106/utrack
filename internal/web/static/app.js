(function() {
  const state = {
    range: 'today',
    fromUnix: 0,
    toUnix: 0,
    fromDate: null,
    toDate: null,
    apps: [],
    daily: [],
    timeline: [],
    expandedApps: new Set(),
    dailyBarRegions: [],
    autoRefreshTimer: null
  };

  const appPalette = [
    '#5e81ac', '#88c0d0', '#81a1c1', '#a3be8c',
    '#ebcb8b', '#d08770', '#b48ead', '#8fbcbb',
    '#d8dee9', '#4c566a', '#bf616a', '#eed49f',
    '#a6da95', '#8aadf4', '#c6a0f6', '#f5bde6'
  ];
  const appColorMap = new Map();

  function getAppColor(appName) {
    if (!appName) return '#5e81ac';
    if (appColorMap.has(appName)) return appColorMap.get(appName);
    const color = appPalette[appColorMap.size % appPalette.length];
    appColorMap.set(appName, color);
    return color;
  }

  function formatDuration(seconds) {
    if (seconds < 60) return `${seconds}s`;
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    if (mins < 60) return `${mins}m ${secs}s`;
    const hrs = Math.floor(mins / 60);
    const remMins = mins % 60;
    return `${hrs}h ${remMins}m`;
  }

  function formatDateDMY(d) {
    return String(d.getDate()).padStart(2, '0') + '/' +
           String(d.getMonth() + 1).padStart(2, '0') + '/' +
           d.getFullYear();
  }

  function updateDateBadge() {
    const badge = document.getElementById('date-display-badge');
    if (!badge || !state.fromDate || !state.toDate) return;
    const fromStr = formatDateDMY(state.fromDate);
    const toStr = formatDateDMY(state.toDate);
    badge.textContent = fromStr === toStr ? fromStr : `${fromStr} – ${toStr}`;
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function toISODate(d) {
    return d.getFullYear() + '-' +
           String(d.getMonth() + 1).padStart(2, '0') + '-' +
           String(d.getDate()).padStart(2, '0');
  }

  function toLocalDateStr(ts) {
    return toISODate(new Date(ts * 1000));
  }

  function countCalendarDays(from, to) {
    const a = new Date(from.getFullYear(), from.getMonth(), from.getDate());
    const b = new Date(to.getFullYear(), to.getMonth(), to.getDate());
    return Math.max(1, Math.round((b - a) / 86400000) + 1);
  }

  function drillIntoDate(dateStr) {
    const parts = dateStr.split('-');
    if (parts.length !== 3) return;
    const from = new Date(+parts[0], +parts[1] - 1, +parts[2], 0, 0, 0);
    const to   = new Date(+parts[0], +parts[1] - 1, +parts[2], 23, 59, 59);

    document.querySelectorAll('button[data-range]').forEach(b => b.classList.remove('active'));
    state.fromDate = from;
    state.toDate = to;
    state.fromUnix = Math.floor(from.getTime() / 1000);
    state.toUnix = Math.floor(to.getTime() / 1000);
    document.getElementById('date-from').value = toISODate(from);
    document.getElementById('date-to').value = toISODate(to);
    updateDateBadge();
    loadData();
  }

  // ── Timestamps ──

  async function calculateTimestamps(preset) {
    const now = new Date();
    let from, to;

    if (preset === 'today') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0);
      to   = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'yesterday') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 0, 0, 0);
      to   = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 23, 59, 59);
    } else if (preset === 'week') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6, 0, 0, 0);
      to   = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'all') {
      try {
        const bRes = await fetch('/api/bounds');
        const bounds = await bRes.json();
        if (bounds && bounds.min_time > 0) {
          const minD = new Date(bounds.min_time * 1000);
          from = new Date(minD.getFullYear(), minD.getMonth(), minD.getDate(), 0, 0, 0);
        } else {
          from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 30, 0, 0, 0);
        }
      } catch (e) {
        from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 30, 0, 0, 0);
      }
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    }

    state.fromDate = from;
    state.toDate = to;
    state.fromUnix = Math.floor(from.getTime() / 1000);
    state.toUnix = Math.floor(to.getTime() / 1000);

    document.getElementById('date-from').value = '';
    document.getElementById('date-to').value = '';
    updateDateBadge();
  }

  // ── Data Loading ──

  async function loadData() {
    try {
      const [summaryRes, dailyRes] = await Promise.all([
        fetch(`/api/summary?from=${state.fromUnix}&to=${state.toUnix}`),
        fetch(`/api/daily?from=${state.fromUnix}&to=${state.toUnix}`)
      ]);
      state.apps = await summaryRes.json() || [];
      state.daily = await dailyRes.json() || [];

      renderStats();
      renderAppList();
      renderDailyPanel();
      await loadTimeline();
    } catch (err) {
      console.error('Failed to load data:', err);
    }
  }

  // ── Stats Cards ──

  function renderStats() {
    const totalSecs = state.apps.reduce((sum, a) => sum + a.duration_s, 0);
    document.getElementById('stat-total-time').textContent = formatDuration(totalSecs);
    document.getElementById('stat-app-count').textContent = state.apps.length;
    document.getElementById('stat-top-app').textContent = state.apps.length > 0 ? state.apps[0].app_id : '—';

    const isMultiDay = (state.toUnix - state.fromUnix) > 90000;
    const avgCard = document.getElementById('card-daily-avg');
    const avgValue = document.getElementById('stat-daily-avg');

    if (isMultiDay && avgCard && avgValue) {
      avgCard.style.display = 'block';
      const daysCount = (state.fromDate && state.toDate)
        ? countCalendarDays(state.fromDate, state.toDate)
        : (state.daily.length || 1);
      avgValue.textContent = formatDuration(Math.round(totalSecs / daysCount));
    } else if (avgCard) {
      avgCard.style.display = 'none';
    }
  }

  // ── App List with Collapsible Titles ──

  function renderAppList() {
    const container = document.getElementById('app-list');
    container.innerHTML = '';

    if (state.apps.length === 0) {
      container.innerHTML = '<div class="empty-state">No tracked activities for this period.</div>';
      return;
    }

    const totalSecs = state.apps.reduce((sum, a) => sum + a.duration_s, 0) || 1;

    state.apps.forEach(app => {
      const pct = Math.round((app.duration_s / totalSecs) * 100);
      const isExpanded = state.expandedApps.has(app.app_id);

      const item = document.createElement('div');
      item.className = 'app-item' + (isExpanded ? ' expanded' : '');

      const row = document.createElement('div');
      row.className = 'app-row';
      row.innerHTML = `
        <div class="app-main">
          <div class="app-title-group">
            <span class="toggle-icon">▶</span>
            <span class="app-name">${escapeHtml(app.app_id)}</span>
          </div>
          <span class="app-time">${formatDuration(app.duration_s)} (${pct}%)</span>
        </div>
        <div class="progress-track">
          <div class="progress-fill" style="width: ${pct}%; background-color: ${getAppColor(app.app_id)}"></div>
        </div>
      `;

      const collapse = document.createElement('div');
      collapse.className = 'app-titles-collapse';

      row.onclick = async () => {
        if (item.classList.contains('expanded')) {
          item.classList.remove('expanded');
          state.expandedApps.delete(app.app_id);
        } else {
          item.classList.add('expanded');
          state.expandedApps.add(app.app_id);
          await fetchTitlesForApp(app.app_id, collapse);
        }
      };

      item.appendChild(row);
      item.appendChild(collapse);
      container.appendChild(item);

      if (isExpanded) fetchTitlesForApp(app.app_id, collapse);
    });
  }

  async function fetchTitlesForApp(appId, el) {
    el.innerHTML = '<div style="color:var(--text-muted); font-size:12px; padding:6px 0;">Loading…</div>';
    try {
      const res = await fetch(`/api/app-titles?app=${encodeURIComponent(appId)}&from=${state.fromUnix}&to=${state.toUnix}`);
      const titles = await res.json() || [];

      el.innerHTML = '';
      if (titles.length === 0) {
        el.innerHTML = '<div style="color:var(--text-muted); font-size:12px; padding:6px 0;">No titles.</div>';
        return;
      }
      titles.forEach(t => {
        const row = document.createElement('div');
        row.className = 'title-row';
        row.innerHTML = `
          <span class="title-name" title="${escapeHtml(t.title)}">${escapeHtml(t.title || '(unnamed)')}</span>
          <span class="title-time">${formatDuration(t.duration_s)}</span>
        `;
        el.appendChild(row);
      });
    } catch (err) {
      el.innerHTML = '<div style="color:var(--accent-red); font-size:12px;">Failed to load titles.</div>';
    }
  }

  // ── Daily Bar Chart ──

  function renderDailyPanel() {
    const panel = document.getElementById('daily-panel');
    if (!panel) return;

    const isMultiDay = (state.toUnix - state.fromUnix) > 90000;
    if (!isMultiDay || state.daily.length === 0) {
      panel.style.display = 'none';
      return;
    }
    panel.style.display = 'block';
    drawDailyChartCanvas();
  }

  function drawDailyChartCanvas() {
    const canvas = document.getElementById('daily-chart-canvas');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const width = canvas.parentElement.clientWidth;
    const height = 180;
    canvas.width = width * window.devicePixelRatio;
    canvas.height = height * window.devicePixelRatio;
    ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
    ctx.clearRect(0, 0, width, height);
    canvas.onmousemove = null;
    canvas.onmouseleave = null;
    canvas.onclick = null;
    canvas.style.cursor = 'default';

    if (state.daily.length === 0) return;

    const pad = { top: 20, right: 20, bottom: 30, left: 45 };
    const chartW = width - pad.left - pad.right;
    const chartH = height - pad.top - pad.bottom;
    const maxHrs = Math.max(...state.daily.map(d => d.duration_s / 3600), 4);
    const yMax = Math.ceil(maxHrs * 1.15);

    // Y-axis grid
    ctx.font = '10px "JetBrains Mono", monospace';
    ctx.textAlign = 'right';
    for (let i = 0; i <= 4; i++) {
      const val = (yMax / 4) * i;
      const y = pad.top + chartH - (val / yMax) * chartH;
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.05)';
      ctx.beginPath();
      ctx.moveTo(pad.left, y);
      ctx.lineTo(width - pad.right, y);
      ctx.stroke();
      ctx.fillStyle = '#6b778c';
      ctx.fillText(`${val.toFixed(1)}h`, pad.left - 8, y + 3);
    }

    // Bars
    const n = state.daily.length;
    const colW = chartW / n;
    const barW = Math.max(8, Math.min(colW * 0.65, 42));
    state.dailyBarRegions = [];

    state.daily.forEach((d, idx) => {
      const hrs = d.duration_s / 3600;
      const barH = Math.max(2, (hrs / yMax) * chartH);
      const x = pad.left + idx * colW + (colW - barW) / 2;
      const y = pad.top + chartH - barH;

      state.dailyBarRegions.push({ x, y, w: barW, h: barH, date: d.date, duration_s: d.duration_s });

      ctx.fillStyle = '#5e81ac';
      const r = Math.min(4, barW / 2);
      ctx.beginPath();
      ctx.moveTo(x, y + barH);
      ctx.lineTo(x, y + r);
      ctx.quadraticCurveTo(x, y, x + r, y);
      ctx.lineTo(x + barW - r, y);
      ctx.quadraticCurveTo(x + barW, y, x + barW, y + r);
      ctx.lineTo(x + barW, y + barH);
      ctx.closePath();
      ctx.fill();

      ctx.textAlign = 'center';
      ctx.fillStyle = '#88c0d0';
      ctx.font = '10px "JetBrains Mono", monospace';
      ctx.fillText(formatDuration(d.duration_s), x + barW / 2, y - 6);

      const parts = d.date.split('-');
      ctx.fillStyle = '#9aa5b5';
      ctx.fillText(parts.length === 3 ? `${parts[2]}/${parts[1]}` : d.date, x + barW / 2, height - 10);
    });

    const hitBar = (e) => {
      const rect = canvas.getBoundingClientRect();
      const sx = (e.clientX - rect.left) * (width / rect.width);
      const sy = (e.clientY - rect.top) * (height / rect.height);

      for (const bar of state.dailyBarRegions) {
        if (sx >= bar.x && sx <= bar.x + bar.w && sy >= bar.y - 10 && sy <= pad.top + chartH + 20) {
          return bar;
        }
      }
      return null;
    };

    // Hover a bar to preview its day, click to drill in
    const hoverInfo = document.getElementById('daily-hover-info');
    const idleHint = 'Click any bar to drill down into that day';
    const setHover = (text) => {
      if (hoverInfo) hoverInfo.textContent = text || idleHint;
    };

    setHover('');
    canvas.onmousemove = function(e) {
      const bar = hitBar(e);
      if (bar) {
        const parts = bar.date.split('-');
        const label = parts.length === 3 ? `${parts[2]}/${parts[1]}` : bar.date;
        setHover(`${label} — ${formatDuration(bar.duration_s)}`);
        canvas.style.cursor = 'pointer';
      } else {
        setHover('');
        canvas.style.cursor = 'default';
      }
    };

    canvas.onmouseleave = function() {
      setHover('');
      canvas.style.cursor = 'default';
    };

    canvas.onclick = function(e) {
      const bar = hitBar(e);
      if (bar) drillIntoDate(bar.date);
    };
  }

  // ── Timeline ──

  async function loadTimeline() {
    try {
      const res = await fetch(`/api/timeline?from=${state.fromUnix}&to=${state.toUnix}&limit=3000`);
      state.timeline = await res.json() || [];
      drawTimelineCanvas();
    } catch (err) {
      console.error('Failed to load timeline:', err);
    }
  }

  function drawTimelineCanvas() {
    const canvas = document.getElementById('timeline-canvas');
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const width = canvas.parentElement.clientWidth;
    const height = 56;
    canvas.width = width * window.devicePixelRatio;
    canvas.height = height * window.devicePixelRatio;
    ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
    ctx.clearRect(0, 0, width, height);
    canvas.onmousemove = null;
    canvas.onmouseleave = null;
    canvas.onclick = null;
    canvas.style.cursor = 'default';

    if (state.timeline.length === 0) {
      ctx.fillStyle = '#9aa5b5';
      ctx.font = '12px sans-serif';
      ctx.fillText('No timeline data available for this range', 16, 26);
      return;
    }

    const tStart = state.fromUnix;
    const tEnd = state.toUnix;
    const span = tEnd - tStart;
    if (span <= 0) return;

    // Activity bars
    state.timeline.forEach(item => {
      const x1 = Math.max(0, ((item.start_time - tStart) / span) * width);
      const x2 = Math.min(width, ((item.end_time - tStart) / span) * width);
      ctx.fillStyle = getAppColor(item.app_id);
      ctx.fillRect(x1, 4, Math.max(2, x2 - x1), 28);
    });

    // X-axis labels
    ctx.font = '10px "JetBrains Mono", monospace';
    ctx.textAlign = 'center';
    const isMultiDay = span > 90000;

    if (!isMultiDay) {
      const sd = new Date(tStart * 1000);
      const dayStart = new Date(sd.getFullYear(), sd.getMonth(), sd.getDate(), 0, 0, 0);
      for (let h = 0; h <= 24; h += 3) {
        const tick = new Date(dayStart.getFullYear(), dayStart.getMonth(), dayStart.getDate(), h, 0, 0);
        const x = ((Math.floor(tick.getTime() / 1000) - tStart) / span) * width;
        if (x >= 0 && x <= width) {
          ctx.fillStyle = '#444d5c';
          ctx.fillRect(x, 34, 1, 4);
          ctx.fillStyle = '#9aa5b5';
          ctx.fillText(String(tick.getHours()).padStart(2, '0') + ':00', x, 48);
        }
      }
    } else {
      const totalDays = Math.ceil(span / 86400);
      let dayStep = 1;
      if (totalDays > 45) dayStep = 7;
      else if (totalDays > 21) dayStep = 3;
      else if (totalDays > 12) dayStep = 2;

      const sd = new Date(tStart * 1000);
      let cur = new Date(sd.getFullYear(), sd.getMonth(), sd.getDate(), 0, 0, 0);
      while (cur.getTime() / 1000 <= tEnd) {
        const x = ((Math.floor(cur.getTime() / 1000) - tStart) / span) * width;
        if (x >= 0 && x <= width) {
          ctx.fillStyle = '#444d5c';
          ctx.fillRect(x, 34, 1, 4);
          ctx.fillStyle = '#9aa5b5';
          ctx.fillText(String(cur.getDate()).padStart(2, '0') + '/' + String(cur.getMonth() + 1).padStart(2, '0'), x, 48);
        }
        cur.setDate(cur.getDate() + dayStep);
      }
    }

    // Hover: compact peek. Multi-day views drop the time range and drill on click.
    const hoverInfo = document.getElementById('timeline-hover-info');
    const idleHint = isMultiDay ? 'Click to view that day' : '';
    const setHover = (text) => {
      if (hoverInfo) hoverInfo.textContent = text || idleHint;
    };
    const fmtTime = (ts) => {
      const d = new Date(ts * 1000);
      return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
    };
    const fmtDate = (ts) => {
      const d = new Date(ts * 1000);
      return String(d.getDate()).padStart(2, '0') + '/' + String(d.getMonth() + 1).padStart(2, '0');
    };
    const clampTitle = (title) => {
      if (!title) return '';
      return title.length > 50 ? title.substring(0, 47) + '…' : title;
    };

    setHover('');

    canvas.onmousemove = function(e) {
      const rect = canvas.getBoundingClientRect();
      const hoverX = ((e.clientX - rect.left) / rect.width) * width;
      const hoverTime = tStart + (hoverX / width) * span;

      // Hit-test the drawn bar (min 2px wide), not the raw time span.
      let hit = null;
      for (const item of state.timeline) {
        const x1 = Math.max(0, ((item.start_time - tStart) / span) * width);
        if (x1 > hoverX) break;
        const x2 = Math.min(width, ((item.end_time - tStart) / span) * width);
        if (hoverX <= x1 + Math.max(2, x2 - x1)) { hit = item; break; }
      }

      if (hit) {
        const title = clampTitle(hit.title);
        if (isMultiDay) {
          setHover(`${fmtDate(hit.start_time)} · ${hit.app_id}${title ? ' — ' + title : ''}`);
        } else {
          setHover(`${hit.app_id}${title ? ' — ' + title : ''}  [${fmtTime(hit.start_time)}–${fmtTime(hit.end_time)}]`);
        }
        canvas.style.cursor = isMultiDay ? 'pointer' : 'crosshair';
      } else if (isMultiDay) {
        setHover(fmtDate(hoverTime));
        canvas.style.cursor = 'pointer';
      } else {
        setHover(fmtTime(hoverTime));
        canvas.style.cursor = 'default';
      }
    };

    canvas.onmouseleave = function() {
      setHover('');
      canvas.style.cursor = 'default';
    };

    // Click a multi-day timeline to open that day
    canvas.onclick = function(e) {
      if (!isMultiDay) return;
      const rect = canvas.getBoundingClientRect();
      const clickTime = tStart + ((e.clientX - rect.left) / rect.width) * span;
      drillIntoDate(toLocalDateStr(clickTime));
    };
  }

  // ── Theme ──

  function initTheme() {
    const saved = localStorage.getItem('utrack-theme') || 'dark';
    applyTheme(saved);
    const btn = document.getElementById('theme-toggle');
    if (btn) {
      btn.onclick = () => {
        const isLight = document.documentElement.getAttribute('data-theme') === 'light';
        applyTheme(isLight ? 'dark' : 'light');
      };
    }
  }

  function applyTheme(theme) {
    const btn = document.getElementById('theme-toggle');
    if (theme === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
      if (btn) btn.textContent = '☀️';
    } else {
      document.documentElement.removeAttribute('data-theme');
      if (btn) btn.textContent = '🌙';
    }
    localStorage.setItem('utrack-theme', theme);
  }

  // ── Event Handlers ──

  document.querySelectorAll('button[data-range]').forEach(btn => {
    btn.onclick = async () => {
      document.querySelectorAll('button[data-range]').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      state.range = btn.getAttribute('data-range');
      await calculateTimestamps(state.range);
      loadData();
    };
  });

  document.getElementById('apply-custom-date').onclick = () => {
    const fromVal = document.getElementById('date-from').value;
    const toVal = document.getElementById('date-to').value;
    if (!fromVal || !toVal) return;

    document.querySelectorAll('button[data-range]').forEach(b => b.classList.remove('active'));
    state.fromDate = new Date(fromVal + 'T00:00:00');
    state.toDate   = new Date(toVal + 'T23:59:59');
    state.fromUnix = Math.floor(state.fromDate.getTime() / 1000);
    state.toUnix   = Math.floor(state.toDate.getTime() / 1000);
    updateDateBadge();
    loadData();
  };

  document.getElementById('btn-refresh').onclick = () => loadData();

  document.getElementById('auto-refresh-toggle').onchange = (e) => {
    if (e.target.checked) {
      state.autoRefreshTimer = setInterval(() => loadData(), 10000);
    } else if (state.autoRefreshTimer) {
      clearInterval(state.autoRefreshTimer);
      state.autoRefreshTimer = null;
    }
  };

  window.addEventListener('resize', () => {
    drawTimelineCanvas();
    drawDailyChartCanvas();
  });

  // ── Init ──

  initTheme();
  (async () => {
    await calculateTimestamps('today');
    loadData();
  })();
})();

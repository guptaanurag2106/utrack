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
    if (appColorMap.has(appName)) {
      return appColorMap.get(appName);
    }
    // Allocate next distinct color from palette based on encounter order
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
    const day = String(d.getDate()).padStart(2, '0');
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const year = d.getFullYear();
    return `${day}/${month}/${year}`;
  }

  function updateDateBadge() {
    const badge = document.getElementById('date-display-badge');
    if (!badge || !state.fromDate || !state.toDate) return;

    const fromStr = formatDateDMY(state.fromDate);
    const toStr = formatDateDMY(state.toDate);
    if (fromStr === toStr) {
      badge.textContent = fromStr;
    } else {
      badge.textContent = `${fromStr} – ${toStr}`;
    }
  }

  async function calculateTimestamps(preset) {
    const now = new Date();
    let from, to;

    if (preset === 'today') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'yesterday') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 23, 59, 59);
    } else if (preset === 'week') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6, 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'all') {
      try {
        const bRes = await fetch('/api/bounds');
        const bounds = await bRes.json();
        if (bounds && bounds.min_time > 0) {
          const minDate = new Date(bounds.min_time * 1000);
          from = new Date(minDate.getFullYear(), minDate.getMonth(), minDate.getDate(), 0, 0, 0);
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

    // Clear custom date inputs on preset switch
    document.getElementById('date-from').value = '';
    document.getElementById('date-to').value = '';

    updateDateBadge();
  }

  async function loadData() {
    try {
      const summaryRes = await fetch(`/api/summary?from=${state.fromUnix}&to=${state.toUnix}`);
      state.apps = await summaryRes.json() || [];

      renderStats();
      renderAppList();
      await loadDailyBreakdown();
      await loadTimeline();
    } catch (err) {
      console.error('Failed to load activity summary:', err);
    }
  }

  async function loadDailyBreakdown() {
    const dailyPanel = document.getElementById('daily-panel');
    if (!dailyPanel) return;

    // Show daily breakdown panel when span is longer than 1 day
    const isMultiDay = (state.toUnix - state.fromUnix) > 90000;
    if (!isMultiDay) {
      dailyPanel.style.display = 'none';
      return;
    }

    dailyPanel.style.display = 'block';

    try {
      const res = await fetch(`/api/daily?from=${state.fromUnix}&to=${state.toUnix}`);
      state.daily = await res.json() || [];
      drawDailyChartCanvas();
    } catch (err) {
      console.error('Failed to load daily breakdown:', err);
    }
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

    if (state.daily.length === 0) {
      ctx.fillStyle = '#9aa5b5';
      ctx.font = '12px sans-serif';
      ctx.fillText('No daily data available for this range', 16, height / 2);
      return;
    }

    const padding = { top: 20, right: 20, bottom: 30, left: 45 };
    const chartW = width - padding.left - padding.right;
    const chartH = height - padding.top - padding.bottom;

    // Convert seconds to hours for Y-axis
    const maxHours = Math.max(...state.daily.map(d => d.duration_s / 3600), 4);
    const yMax = Math.ceil(maxHours * 1.15);

    // Draw horizontal grid lines & Y-axis labels
    ctx.font = '10px "JetBrains Mono", monospace, sans-serif';
    ctx.textAlign = 'right';
    const gridSteps = 4;
    for (let i = 0; i <= gridSteps; i++) {
      const val = (yMax / gridSteps) * i;
      const y = padding.top + chartH - (val / yMax) * chartH;

      ctx.strokeStyle = 'rgba(255, 255, 255, 0.05)';
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(width - padding.right, y);
      ctx.stroke();

      ctx.fillStyle = '#6b778c';
      ctx.fillText(`${val.toFixed(1)}h`, padding.left - 8, y + 3);
    }

    // Draw vertical bars
    const n = state.daily.length;
    const colW = chartW / n;
    const barW = Math.max(8, Math.min(colW * 0.65, 42));

    state.daily.forEach((d, idx) => {
      const hrs = d.duration_s / 3600;
      const barH = (hrs / yMax) * chartH;
      const x = padding.left + idx * colW + (colW - barW) / 2;
      const y = padding.top + chartH - barH;

      // Bar rounded top
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

      // Top value label
      ctx.textAlign = 'center';
      ctx.fillStyle = '#88c0d0';
      ctx.font = '10px "JetBrains Mono", monospace';
      ctx.fillText(formatDuration(d.duration_s), x + barW / 2, y - 6);

      // Bottom date label
      const parts = d.date.split('-');
      const dateLabel = parts.length === 3 ? `${parts[2]}/${parts[1]}` : d.date;
      ctx.fillStyle = '#9aa5b5';
      ctx.fillText(dateLabel, x + barW / 2, height - 10);
    });
  }

  function renderStats() {
    const totalSecs = state.apps.reduce((sum, a) => sum + a.duration_s, 0);
    document.getElementById('stat-total-time').textContent = formatDuration(totalSecs);
    document.getElementById('stat-app-count').textContent = state.apps.length;
    document.getElementById('stat-top-app').textContent = state.apps.length > 0 ? state.apps[0].app_id : '—';
  }

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
      collapse.id = `titles-${encodeURIComponent(app.app_id)}`;

      row.onclick = async () => {
        const currentlyExpanded = item.classList.contains('expanded');
        if (currentlyExpanded) {
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

      if (isExpanded) {
        fetchTitlesForApp(app.app_id, collapse);
      }
    });
  }

  async function fetchTitlesForApp(appId, containerEl) {
    containerEl.innerHTML = '<div style="color:var(--text-muted); font-size:12px; padding:6px 0;">Loading window titles...</div>';
    try {
      const res = await fetch(`/api/app-titles?app=${encodeURIComponent(appId)}&from=${state.fromUnix}&to=${state.toUnix}`);
      const titles = await res.json() || [];

      if (titles.length === 0) {
        containerEl.innerHTML = '<div style="color:var(--text-muted); font-size:12px; padding:6px 0;">No titles recorded.</div>';
        return;
      }

      containerEl.innerHTML = '';
      titles.forEach(t => {
        const row = document.createElement('div');
        row.className = 'title-row';
        row.innerHTML = `
          <span class="title-name" title="${escapeHtml(t.title)}">${escapeHtml(t.title || '(unnamed)')}</span>
          <span class="title-time">${formatDuration(t.duration_s)}</span>
        `;
        containerEl.appendChild(row);
      });
    } catch (err) {
      containerEl.innerHTML = '<div style="color:var(--accent-red); font-size:12px;">Failed to load titles.</div>';
    }
  }

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

    // Draw activity intervals
    state.timeline.forEach(item => {
      const x1 = Math.max(0, ((item.start_time - tStart) / span) * width);
      const x2 = Math.min(width, ((item.end_time - tStart) / span) * width);
      const w = Math.max(2, x2 - x1);

      ctx.fillStyle = getAppColor(item.app_id);
      ctx.fillRect(x1, 4, w, 28);
    });

    ctx.font = '10px "JetBrains Mono", monospace, sans-serif';
    ctx.textAlign = 'center';

    const isMultiDay = span > 90000;

    if (!isMultiDay) {
      // Single-day view: show 3-hour interval ticks in local time (00:00, 03:00, ..., 24:00)
      const startDate = new Date(tStart * 1000);
      const localDayStart = new Date(startDate.getFullYear(), startDate.getMonth(), startDate.getDate(), 0, 0, 0);

      for (let h = 0; h <= 24; h += 3) {
        const tickDate = new Date(localDayStart.getFullYear(), localDayStart.getMonth(), localDayStart.getDate(), h, 0, 0);
        const tickSec = Math.floor(tickDate.getTime() / 1000);
        const x = ((tickSec - tStart) / span) * width;

        if (x >= 0 && x <= width) {
          ctx.fillStyle = '#444d5c';
          ctx.fillRect(x, 34, 1, 4);

          const hrs = String(tickDate.getHours()).padStart(2, '0');
          const mins = String(tickDate.getMinutes()).padStart(2, '0');
          ctx.fillStyle = '#9aa5b5';
          ctx.fillText(`${hrs}:${mins}`, x, 48);
        }
      }
    } else {
      // Multi-day view: calculate appropriate day step so labels don't collide
      const totalDays = Math.ceil(span / 86400);
      let dayStep = 1;
      if (totalDays > 45) dayStep = 7;
      else if (totalDays > 21) dayStep = 3;
      else if (totalDays > 12) dayStep = 2;

      const startDate = new Date(tStart * 1000);
      let cur = new Date(startDate.getFullYear(), startDate.getMonth(), startDate.getDate(), 0, 0, 0);

      while (cur.getTime() / 1000 <= tEnd) {
        const tickSec = Math.floor(cur.getTime() / 1000);
        const x = ((tickSec - tStart) / span) * width;

        if (x >= 0 && x <= width) {
          ctx.fillStyle = '#444d5c';
          ctx.fillRect(x, 34, 1, 4);

          const day = String(cur.getDate()).padStart(2, '0');
          const mon = String(cur.getMonth() + 1).padStart(2, '0');
          ctx.fillStyle = '#9aa5b5';
          ctx.fillText(`${day}/${mon}`, x, 48);
        }

        cur.setDate(cur.getDate() + dayStep);
      }
    }
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function initTheme() {
    const saved = localStorage.getItem('utrack-theme') || 'dark';
    applyTheme(saved);

    const toggleBtn = document.getElementById('theme-toggle');
    if (toggleBtn) {
      toggleBtn.onclick = () => {
        const isLight = document.documentElement.getAttribute('data-theme') === 'light';
        applyTheme(isLight ? 'dark' : 'light');
      };
    }
  }

  function applyTheme(theme) {
    const toggleBtn = document.getElementById('theme-toggle');
    if (theme === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
      if (toggleBtn) toggleBtn.textContent = '☀️';
    } else {
      document.documentElement.removeAttribute('data-theme');
      if (toggleBtn) toggleBtn.textContent = '🌙';
    }
    localStorage.setItem('utrack-theme', theme);
  }

  // Event handlers
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
    const from = new Date(fromVal + 'T00:00:00');
    const to = new Date(toVal + 'T23:59:59');
    state.fromDate = from;
    state.toDate = to;
    state.fromUnix = Math.floor(from.getTime() / 1000);
    state.toUnix = Math.floor(to.getTime() / 1000);
    updateDateBadge();
    loadData();
  };

  document.getElementById('btn-refresh').onclick = () => {
    loadData();
  };

  const autoToggle = document.getElementById('auto-refresh-toggle');
  autoToggle.onchange = (e) => {
    if (e.target.checked) {
      state.autoRefreshTimer = setInterval(() => loadData(), 10000);
    } else {
      if (state.autoRefreshTimer) {
        clearInterval(state.autoRefreshTimer);
        state.autoRefreshTimer = null;
      }
    }
  };

  window.addEventListener('resize', () => {
    drawTimelineCanvas();
    drawDailyChartCanvas();
  });

  // Initialization
  initTheme();
  (async () => {
    await calculateTimestamps('today');
    loadData();
  })();
})();

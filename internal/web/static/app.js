(function() {
  const state = {
    range: 'today',
    fromUnix: 0,
    toUnix: 0,
    apps: [],
    timeline: [],
    expandedApps: new Set(),
    autoRefreshTimer: null
  };

  const appColors = [
    '#5e81ac', '#88c0d0', '#81a1c1', '#a3be8c',
    '#ebcb8b', '#d08770', '#b48ead', '#8fbcbb'
  ];

  function getAppColor(appName) {
    let hash = 0;
    for (let i = 0; i < appName.length; i++) {
      hash = (hash << 5) - hash + appName.charCodeAt(i);
      hash |= 0;
    }
    return appColors[Math.abs(hash) % appColors.length];
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

  function calculateTimestamps(preset) {
    const now = new Date();
    let from, to;

    if (preset === 'today') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'yesterday') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1, 23, 59, 59);
    } else if (preset === 'week') {
      from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 7, 0, 0, 0);
      to = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 23, 59, 59);
    } else if (preset === 'all') {
      from = new Date(2000, 0, 1);
      to = new Date(now.getFullYear() + 1, 0, 1);
    }

    state.fromUnix = Math.floor(from.getTime() / 1000);
    state.toUnix = Math.floor(to.getTime() / 1000);
  }

  async function loadData() {
    try {
      const summaryRes = await fetch(`/api/summary?from=${state.fromUnix}&to=${state.toUnix}`);
      state.apps = await summaryRes.json() || [];

      renderStats();
      renderAppList();
      await loadTimeline();
    } catch (err) {
      console.error('Failed to load activity summary:', err);
    }
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
    const height = 48;
    canvas.width = width * window.devicePixelRatio;
    canvas.height = height * window.devicePixelRatio;
    ctx.scale(window.devicePixelRatio, window.devicePixelRatio);

    ctx.clearRect(0, 0, width, height);

    if (state.timeline.length === 0) {
      ctx.fillStyle = '#9aa5b5';
      ctx.font = '12px sans-serif';
      ctx.fillText('No timeline data available for this range', 16, 28);
      return;
    }

    const tStart = state.fromUnix;
    const tEnd = state.toUnix;
    const span = tEnd - tStart;
    if (span <= 0) return;

    state.timeline.forEach(item => {
      const x1 = Math.max(0, ((item.start_time - tStart) / span) * width);
      const x2 = Math.min(width, ((item.end_time - tStart) / span) * width);
      const w = Math.max(2, x2 - x1);

      ctx.fillStyle = getAppColor(item.app_id);
      ctx.fillRect(x1, 8, w, height - 16);
    });
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  // Event handlers
  document.querySelectorAll('button[data-range]').forEach(btn => {
    btn.onclick = () => {
      document.querySelectorAll('button[data-range]').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      state.range = btn.getAttribute('data-range');
      calculateTimestamps(state.range);
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
    state.fromUnix = Math.floor(from.getTime() / 1000);
    state.toUnix = Math.floor(to.getTime() / 1000);
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
  });

  // Init
  calculateTimestamps('today');
  loadData();
})();

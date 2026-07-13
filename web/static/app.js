// ---- display manifest (optional hints; unknown components render generically) ----
const MANIFEST = {
  cpu: { title: 'CPU', headline: 'cpu_usage', headlineLabel: 'CPU 使用率 (%)',
         key: [ {name:'usage', prefer:{core:'total'}}, 'load_average', 'temperature', 'model_info' ] },
  memory: { title: '内存', headline: 'memory_usage', headlineLabel: '内存使用率 (%)',
            key: [ 'usage', 'swap_usage', 'oom_count', 'page_faults' ] },
  disk: { title: '磁盘', headline: 'disk_space_usage', headlineLabel: '磁盘使用率 (%)',
          key: [ 'space_usage', 'throughput', 'io_wait', 'io_errors' ] },
  gpu: { title: 'GPU', headline: 'gpu_utilization', headlineLabel: 'GPU 使用率 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  npu: { title: 'NPU', headline: 'npu_utilization', headlineLabel: 'NPU 使用率 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  network: { title: '网络', headline: null,
             key: [ 'rx_bytes_total', 'tx_bytes_total', 'error_count', 'connection_count' ] },
};

const METRIC_NAMES = {
  usage: '使用率', load_average: '负载', context_switches: '上下文切换',
  process_count: '进程数', model_info: '型号', temperature: '温度', frequency: '频率',
  space_usage: '空间使用率', space_detail: '空间明细', throughput: '吞吐量',
  io_wait: 'IO Wait', io_errors: 'IO 错误', iops: 'IOPS',
  smart_status: 'SMART', smart_temperature: 'SMART 温度',
  memory_usage: '显存使用率', memory_detail: '明细',
  power_draw: '功耗', fan_speed: '风扇', ecc_errors: 'ECC 错误',
  clock_frequency: '频率', utilization: '使用率', health_status: '健康状态',
  swap_usage: 'Swap 使用率', oom_count: 'OOM 次数', page_faults: '页错误',
  rx_bytes_total: '接收字节', tx_bytes_total: '发送字节',
  error_count: '错误计数', connection_count: '连接数',
  interface_status: '接口状态', usage_detail: '明细', packet_count: '包计数',
};

const SERIES_LABELS = {
  cpu_usage: 'CPU 使用率 (%)', cpu_load_average: '负载 (1m)',
  memory_usage: '内存使用率 (%)', memory_swap_usage: 'Swap 使用率 (%)',
  disk_space_usage: '磁盘使用率 (%)',
  gpu_utilization: 'GPU 使用率 (%)', gpu_memory_usage: 'GPU 显存使用率 (%)', gpu_temperature: 'GPU 温度 (°C)',
  npu_utilization: 'NPU 使用率 (%)', npu_memory_usage: 'NPU 显存使用率 (%)', npu_temperature: 'NPU 温度 (°C)',
};

const NAV_ORDER = ['cpu', 'memory', 'disk', 'gpu', 'npu', 'network'];

// ---- state ----
let collectors = [];
let lastSnapshot = null;
let refreshIntervalMs = 5000;
let pollTimer = null;
let autoOn = true;

// ---- helpers ----
function el(tag, cls) { const e = document.createElement(tag); if (cls) e.className = cls; return e; }
function elText(tag, cls, text) { const e = el(tag, cls); e.textContent = text; return e; }
function anchor(href, text, cls) { const a = el('a', cls); a.href = href; a.textContent = text; return a; }

function compTitle(key) { return (MANIFEST[key] || {}).title || key.toUpperCase(); }
function navOrder(key) { const i = NAV_ORDER.indexOf(key); return i < 0 ? 999 : i; }

function statusOf(score, max) {
  if (!max) return { label: 'N/A', color: '#9ca3af' };
  const r = score / max;
  if (r >= 0.9) return { label: 'OK', color: '#2e7d32' };
  if (r >= 0.75) return { label: 'Good', color: '#689f38' };
  if (r >= 0.6) return { label: 'Warning', color: '#f57c00' };
  return { label: 'Critical', color: '#c62828' };
}
function gradeColor(grade) {
  if (grade === 'Excellent') return '#2e7d32';
  if (grade === 'Good') return '#689f38';
  if (grade === 'Warning') return '#f57c00';
  return '#c62828';
}
function fmt(v) {
  if (v === null || v === undefined) return '-';
  if (Number.isInteger(v)) return String(v);
  return Number(v).toFixed(2);
}
function seriesLabel(k) {
  if (SERIES_LABELS[k]) return SERIES_LABELS[k];
  return k.replace(/^[a-z]+_/, '').replace(/_/g, ' ');
}

function sparkline(series, color) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 100 30');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.classList.add('spark');
  const n = series.length;
  if (n < 2) return svg;
  let min = Infinity, max = -Infinity;
  for (const v of series) { if (v < min) min = v; if (v > max) max = v; }
  if (max === min) max = min + 1;
  const pts = series.map((v, i) => {
    const x = (i / (n - 1)) * 100;
    const y = 28 - ((v - min) / (max - min)) * 24 - 2;
    return x.toFixed(2) + ',' + y.toFixed(2);
  }).join(' ');
  const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  poly.setAttribute('points', pts);
  poly.setAttribute('fill', 'none');
  poly.setAttribute('stroke', color);
  poly.setAttribute('stroke-width', '1.5');
  poly.setAttribute('vector-effect', 'non-scaling-stroke');
  svg.appendChild(poly);
  return svg;
}

function metricsFor(snap, compKey) { return (snap.metrics || []).filter(m => m.component === compKey); }

function pickMetric(metrics, spec) {
  const name = typeof spec === 'string' ? spec : spec.name;
  const prefer = typeof spec === 'string' ? null : spec.prefer;
  let first = null;
  for (const m of metrics) {
    if (m.name !== name) continue;
    if (!first) first = m;
    if (prefer) {
      let match = true;
      for (const k in prefer) { if ((m.labels || {})[k] !== prefer[k]) { match = false; break; } }
      if (match) return m;
    }
  }
  return first;
}

function orderedComponents() {
  return collectors.slice().sort((a, b) => {
    const oa = navOrder(a.component), ob = navOrder(b.component);
    if (oa !== ob) return oa - ob;
    return a.component < b.component ? -1 : 1;
  });
}

function componentSeries(compKey, history) {
  const prefix = compKey + '_';
  const out = [];
  for (const k in history) {
    if (k.indexOf(prefix) === 0 && history[k].length > 0) {
      out.push({ key: k, label: seriesLabel(k), data: history[k] });
    }
  }
  return out;
}

function currentRoute() {
  const h = location.hash.replace(/^#/, '');
  if (!h || h === '/') return 'overview';
  return h.replace(/^\//, '');
}
function navigate(key) { location.hash = '/' + key; }

// ---- topbar pill ----
function renderPill(snap) {
  const h = snap.health || {};
  const color = gradeColor(h.grade);
  document.getElementById('pillScore').textContent = h.score !== undefined ? h.score : '--';
  document.getElementById('pillGrade').textContent = h.grade || '--';
  document.getElementById('pillGrade').style.color = color;
  document.getElementById('pillDot').style.background = color;
}

// ---- nav ----
function renderNav() {
  const nav = document.getElementById('nav');
  nav.innerHTML = '';
  const route = currentRoute();
  const aOverview = el('a'); aOverview.href = '#/'; aOverview.textContent = '概览';
  if (route === 'overview') aOverview.className = 'active';
  nav.appendChild(aOverview);
  for (const c of orderedComponents()) {
    const a = el('a'); a.href = '#/' + c.component; a.textContent = compTitle(c.component);
    if (route === c.component) a.className = 'active';
    nav.appendChild(a);
  }
}

// ---- overview ----
function renderOverview(snap) {
  const page = document.getElementById('page');
  page.innerHTML = '';
  const h = snap.health || {};
  const stColor = gradeColor(h.grade);
  const scorePct = (h.score || 0);

  const hero = el('div', 'hero');
  const sc = el('div', 'hero-score');
  sc.innerHTML =
    '<div class="hero-num" style="color:' + stColor + '">' +
      '<span>' + (h.score !== undefined ? h.score : '--') + '</span><span class="max">/100</span>' +
    '</div>' +
    '<div class="hero-grade" style="color:' + stColor + '">' + (h.grade || '--') + '</div>' +
    '<div class="hero-bar"><div class="fill" style="width:' + scorePct + '%;background:' + stColor + '"></div></div>';
  hero.appendChild(sc);

  const info = el('div', 'hero-info');
  info.innerHTML =
    '<div>服务器类型: <b>' + (h.server_type || '--') + '</b></div>' +
    '<div>更新时间: <b>' + (snap.timestamp ? new Date(snap.timestamp).toLocaleString('zh-CN') : '--') + '</b></div>' +
    '<div>采集间隔: <b>' + (snap.refresh_interval_ms ? snap.refresh_interval_ms / 1000 + 's' : '--') + '</b></div>';
  const comps = orderedComponents();
  if (comps.length) {
    const chips = el('div', 'hero-components');
    for (const c of comps) {
      const ch = (h.components || {})[c.component];
      const color = ch ? statusOf(ch.score, ch.max).color : '#9ca3af';
      const chip = el('div', 'comp-chip');
      chip.style.cursor = 'pointer';
      chip.innerHTML = '<span class="dot" style="background:' + color + '"></span>' + compTitle(c.component);
      chip.onclick = () => navigate(c.component);
      chips.appendChild(chip);
    }
    info.appendChild(chips);
  }
  hero.appendChild(info);
  page.appendChild(hero);

  const title = elText('div', 'section-title', '部件概览');
  title.innerHTML = '部件概览 <span class="hint">点击卡片或芯片进入详情</span>';
  page.appendChild(title);

  const grid = el('div', 'grid');
  for (const c of comps) grid.appendChild(summaryCard(c.component, snap));
  page.appendChild(grid);
}

function summaryCard(compKey, snap) {
  const m = MANIFEST[compKey] || {};
  const compHealth = (snap.health && snap.health.components) ? snap.health.components[compKey] : null;
  const metrics = metricsFor(snap, compKey);
  const st = statusOf(compHealth ? compHealth.score : 0, compHealth ? compHealth.max : 0);

  const card = el('div', 'card');
  card.onclick = () => navigate(compKey);
  const head = el('div', 'card-head');
  const t = el('div', 'card-title');
  t.innerHTML = '<span class="dot" style="background:' + st.color + '"></span>' + compTitle(compKey);
  const sc = el('div', 'card-score');
  if (compHealth) {
    sc.innerHTML = '<b style="color:' + st.color + '">' + compHealth.score + '</b> / ' + compHealth.max +
      ' <span class="badge" style="background:' + st.color + '">' + st.label + '</span>';
  } else {
    sc.innerHTML = '<span class="badge na">无数据</span>';
  }
  head.appendChild(t); head.appendChild(sc);
  card.appendChild(head);

  const body = el('div', 'card-body');
  if (m.headline && snap.history && snap.history[m.headline] && snap.history[m.headline].length > 1) {
    body.appendChild(elText('div', 'spark-label', m.headlineLabel || ''));
    body.appendChild(sparkline(snap.history[m.headline], st.color));
  }
  if (metrics.length === 0) {
    body.appendChild(elText('div', 'empty', '无数据'));
  } else {
    const kv = el('div', 'kv');
    const keys = m.key || metrics.slice(0, 4).map(x => x.name);
    for (const spec of keys) {
      const mm = pickMetric(metrics, spec);
      if (!mm) continue;
      kv.appendChild(elText('div', 'k', METRIC_NAMES[mm.name] || mm.name));
      const v = el('div', 'v'); v.textContent = fmt(mm.value) + ' ' + (mm.unit || '');
      kv.appendChild(v);
    }
    body.appendChild(kv);
  }
  card.appendChild(body);
  return card;
}

// ---- detail ----
function renderDetail(compKey, snap) {
  const page = document.getElementById('page');
  page.innerHTML = '';

  if (!collectors.some(c => c.component === compKey)) {
    const head = el('div', 'detail-head');
    head.appendChild(anchor('#/', '← 概览', 'back'));
    head.appendChild(elText('span', 'detail-title', '未找到该部件'));
    page.appendChild(head);
    page.appendChild(elText('div', 'empty', '部件 "' + compKey + '" 未注册'));
    return;
  }

  const m = MANIFEST[compKey] || {};
  const compHealth = (snap.health && snap.health.components) ? snap.health.components[compKey] : null;
  const metrics = metricsFor(snap, compKey);
  const st = statusOf(compHealth ? compHealth.score : 0, compHealth ? compHealth.max : 0);

  const head = el('div', 'detail-head');
  head.appendChild(anchor('#/', '← 概览', 'back'));
  head.appendChild(elText('span', 'detail-title', compTitle(compKey)));
  const sc = el('div', 'detail-score');
  if (compHealth) {
    sc.innerHTML = '<b style="color:' + st.color + '">' + compHealth.score + '</b> / ' + compHealth.max +
      ' <span class="badge" style="background:' + st.color + '">' + st.label + '</span>';
  } else {
    sc.innerHTML = '<span class="badge na">无数据</span>';
  }
  head.appendChild(sc);
  page.appendChild(head);

  if (compHealth && compHealth.deductions && compHealth.deductions.length) {
    const d = el('div', 'deductions');
    for (const dd of compHealth.deductions) d.appendChild(elText('div', '', dd.rule + ' (-' + dd.penalty + ')'));
    page.appendChild(d);
  }

  // trends
  const series = componentSeries(compKey, snap.history || {});
  if (series.length) {
    const panel = el('div', 'panel');
    const ph = el('div', 'panel-head');
    ph.appendChild(elText('span', '', '趋势'));
    ph.appendChild(elText('span', 'sub', '近 ' + (snap.history_points || 60) + ' 个采样点'));
    panel.appendChild(ph);
    const body = el('div', 'panel-body trend');
    for (const s of series) {
      const cur = s.data[s.data.length - 1];
      const item = el('div', 'trend-item');
      item.appendChild(elText('div', 'tlabel', s.label));
      item.appendChild(elText('div', 'tval', fmt(cur)));
      item.appendChild(sparkline(s.data, st.color));
      body.appendChild(item);
    }
    panel.appendChild(body);
    page.appendChild(panel);
  }

  // all metrics
  const mpanel = el('div', 'panel');
  const mph = el('div', 'panel-head');
  mph.appendChild(elText('span', '', '全部指标'));
  mph.appendChild(elText('span', 'sub', metrics.length + ' 条'));
  mpanel.appendChild(mph);
  const mbody = el('div', 'panel-body');
  if (metrics.length === 0) {
    mbody.appendChild(elText('div', 'empty', '无数据（采集器不可用或无硬件）'));
  } else {
    const tbl = document.createElement('table');
    tbl.className = 'table';
    tbl.innerHTML = '<thead><tr><th>指标</th><th>值</th><th>标签</th></tr></thead>';
    const tb = document.createElement('tbody');
    for (const mt of metrics) {
      const labels = mt.labels ? Object.entries(mt.labels).map(([k, v]) => k + '=' + v).join(', ') : '';
      const tr = document.createElement('tr');
      tr.innerHTML =
        '<td class="m-name">' + (METRIC_NAMES[mt.name] || mt.name) + '</td>' +
        '<td class="m-val">' + fmt(mt.value) + ' ' + (mt.unit || '') + '</td>' +
        '<td class="m-labels">' + labels + '</td>';
      tb.appendChild(tr);
    }
    tbl.appendChild(tb);
    mbody.appendChild(tbl);
  }
  mpanel.appendChild(mbody);
  page.appendChild(mpanel);
}

// ---- render dispatch ----
function render() {
  renderNav();
  if (!lastSnapshot) {
    document.getElementById('page').innerHTML = '<div class="empty">正在加载数据…</div>';
    return;
  }
  renderPill(lastSnapshot);
  const route = currentRoute();
  if (route === 'overview') renderOverview(lastSnapshot);
  else renderDetail(route, lastSnapshot);
}

// ---- data fetching ----
async function fetchCollectors() {
  try {
    const r = await fetch('/api/collectors', { cache: 'no-store' });
    if (r.ok) collectors = await r.json();
  } catch (e) { /* server starting */ }
}

async function fetchSnapshot() {
  try {
    const r = await fetch('/api/snapshot', { cache: 'no-store' });
    if (!r.ok) { showBanner('快照尚未就绪，等待首次采集…', true); return; }
    lastSnapshot = await r.json();
    hideBanner();
    render();
  } catch (e) {
    showBanner('获取数据失败：' + e.message, true);
  }
}

async function fetchConfigData() {
  try {
    const r = await fetch('/api/config', { cache: 'no-store' });
    if (!r.ok) return;
    const c = await r.json();
    refreshIntervalMs = c.refresh_interval_ms || 5000;
    document.getElementById('intervalInput').value = Math.round(refreshIntervalMs / 1000);
  } catch (e) { /* ignore */ }
}

function startPolling() {
  stopPolling();
  if (!autoOn) return;
  pollTimer = setInterval(fetchSnapshot, refreshIntervalMs);
  fetchSnapshot();
}
function stopPolling() { if (pollTimer) { clearInterval(pollTimer); pollTimer = null; } }

async function applyInterval() {
  const sec = parseInt(document.getElementById('intervalInput').value, 10);
  if (!sec || sec < 1) { showBanner('请输入有效的刷新间隔（秒）', true); return; }
  const ms = sec * 1000;
  try {
    const r = await fetch('/api/config', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_interval_ms: ms }),
    });
    if (!r.ok) { const t = await r.text(); showBanner('应用失败：' + t, true); return; }
    const c = await r.json();
    refreshIntervalMs = c.refresh_interval_ms || ms;
    startPolling();
    showBanner('刷新间隔已更新为 ' + (refreshIntervalMs / 1000) + ' 秒', false);
  } catch (e) {
    showBanner('应用失败：' + e.message, true);
  }
}

async function manualRefresh() {
  try { await fetch('/api/refresh', { method: 'POST' }); } catch (e) { /* ignore */ }
  setTimeout(fetchSnapshot, 400);
}

function showBanner(msg, isError) {
  const b = document.getElementById('banner');
  b.textContent = msg;
  b.classList.remove('hidden');
  b.style.background = isError ? '#fee2e2' : '#dcfce7';
  b.style.color = isError ? '#991b1b' : '#166534';
}
function hideBanner() { document.getElementById('banner').classList.add('hidden'); }

// ---- wiring ----
document.getElementById('applyBtn').addEventListener('click', applyInterval);
document.getElementById('refreshBtn').addEventListener('click', manualRefresh);
document.getElementById('autoToggle').addEventListener('change', (e) => {
  autoOn = e.target.checked;
  if (autoOn) startPolling(); else stopPolling();
});
document.getElementById('intervalInput').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') applyInterval();
});
window.addEventListener('hashchange', render);

(async function init() {
  await fetchCollectors();
  await fetchConfigData();
  startPolling();
})();

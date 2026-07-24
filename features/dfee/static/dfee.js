// ---- config ----
const HISTORY_POINTS = 60;
const PALETTE = [
  '#2563eb', '#dc2626', '#16a34a', '#ea580c', '#9333ea', '#0891b2',
  '#ca8a04', '#db2777', '#4f46e5', '#059669', '#b45309', '#6b7280',
];

const SECTIONS = [
  { title: 'NPU', accent: '#2563eb', ids: ['npu_npu_util', 'npu_utilization', 'npu_vector_core_util', 'npu_memory_usage', 'npu_hbm_bandwidth_util', 'npu_aicore_freq', 'npu_hbm_freq', 'npu_power_draw', 'npu_voltage'], gridCols: 6, spans: { 'npu_npu_util': 2, 'npu_utilization': 2, 'npu_vector_core_util': 2, 'npu_memory_usage': 3, 'npu_hbm_bandwidth_util': 3, 'npu_aicore_freq': 3, 'npu_hbm_freq': 3, 'npu_power_draw': 3, 'npu_voltage': 3 }, filterLabel: 'NPU CARD ID', filterKey: 'npu_', filterPrefix: 'NPU ' },
  { title: 'CPU', accent: '#16a34a', ids: ['cpu_utilization', 'cpu_load', 'cpu_power'] },
  { title: '内存', accent: '#9333ea', ids: ['memory_pool', 'memory_swap'] },
  { title: '磁盘', accent: '#ea580c', ids: ['disk_throughput_read', 'disk_throughput_write', 'disk_iops_read', 'disk_iops_write', 'disk_read_latency', 'disk_write_latency'], gridCols: 2, filterLabel: 'DISK', filterKey: 'disk_' },
  { title: '网络', accent: '#0891b2', ids: ['network_rx', 'network_tx'], gridCols: 2, filterLabel: 'NIC', filterKey: 'network_' },
  { title: '机箱', accent: '#92400e', ids: ['chassis_power', 'chassis_temp', 'chassis_fan'], gridCols: 3 },
];

// ---- state ----
let refreshIntervalMs = 5000;
let pollTimer = null;
let buffers = {};
let chartDefs = {};
let canvasMap = {};
let legendMap = {};
let badgeMap = {};
let filterSets = {}; // {filterKey: Set or null} — null = all visible
let cardOrders = {}; // {sectionTitle: [chartId, ...]}
let cardSizes = {}; // {chartId: {span: N, height: N}}
let dragSource = null;

function loadCardLayout() {
  try { cardOrders = JSON.parse(localStorage.getItem('dfee-card-order') || '{}'); } catch (_) {}
  try { cardSizes = JSON.parse(localStorage.getItem('dfee-card-size') || '{}'); } catch (_) {}
}
function saveCardOrder() { localStorage.setItem('dfee-card-order', JSON.stringify(cardOrders)); }
function saveCardSize() { localStorage.setItem('dfee-card-size', JSON.stringify(cardSizes)); }
function getOrderedIds(sec) {
  const saved = cardOrders[sec.title] || [];
  return [
    ...saved.filter(id => sec.ids.includes(id)),
    ...sec.ids.filter(id => !saved.includes(id))
  ];
}

function loadFilterSet(key) {
  try {
    const saved = JSON.parse(localStorage.getItem('dfee-filter-' + key) || '[]');
    if (saved.length > 0) filterSets[key] = new Set(saved);
  } catch (_) {}
}
function saveFilterSet(key) {
  const set = filterSets[key];
  localStorage.setItem('dfee-filter-' + key, set ? JSON.stringify([...set]) : '[]');
}
function getFilterIds(charts) {
  const ids = new Set();
  for (const c of charts) {
    for (const s of (c.series || [])) {
      const parts = s.id.split(':');
      if (parts.length > 1) ids.add(parts[0]);
    }
  }
  return [...ids].sort((a, b) => a.localeCompare(b, undefined, {numeric: true}));
}
function isSeriesVisible(chart, series) {
  for (const sec of SECTIONS) {
    if (sec.filterKey && chart.id.startsWith(sec.filterKey)) {
      const set = filterSets[sec.filterKey];
      if (!set) return true;
      return set.has(series.id.split(':')[0]);
    }
  }
  return true;
}
function filterDisplayLabel(key) {
  const set = filterSets[key];
  if (!set) return '全部';
  return [...set].sort((a, b) => a.localeCompare(b, undefined, {numeric: true})).join(', ');
}
function buildFilterDropdown(sec, ids) {
  const wrap = el('div', 'npu-filter');
  const label = elText('span', 'filter-label', sec.filterLabel);
  const dropdown = el('div', 'npu-dropdown');
  const trigger = el('button', 'npu-dropdown-trigger');
  trigger.type = 'button';
  trigger.title = filterDisplayLabel(sec.filterKey);
  const textSpan = elText('span', 'npu-dropdown-text', filterDisplayLabel(sec.filterKey));
  trigger.appendChild(textSpan);
  const arrow = elText('span', 'npu-dropdown-arrow', '\u25BE');
  trigger.appendChild(arrow);
  const menu = el('div', 'npu-dropdown-menu');
  menu.onclick = (e) => e.stopPropagation();
  for (const id of ids) {
    const item = el('label', 'npu-dropdown-item');
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = id;
    cb.checked = !filterSets[sec.filterKey] || filterSets[sec.filterKey].has(id);
    cb.onchange = () => onFilterCheckboxChange(sec, ids, trigger, menu);
    item.appendChild(cb);
    item.appendChild(document.createTextNode(' ' + (sec.filterPrefix || '') + id));
    menu.appendChild(item);
  }
  trigger.onclick = (e) => {
    e.stopPropagation();
    menu.classList.toggle('open');
  };
  document.addEventListener('click', function closeMenu(e) {
    if (!wrap.contains(e.target)) menu.classList.remove('open');
  });
  wrap.appendChild(label);
  wrap.appendChild(dropdown);
  dropdown.appendChild(trigger);
  dropdown.appendChild(menu);
  return wrap;
}
function onFilterCheckboxChange(sec, ids, trigger, menu) {
  const checked = ids.filter(id => {
    const cb = menu.querySelector('input[value="' + CSS.escape(id) + '"]');
    return cb && cb.checked;
  });
  if (checked.length === 0 || checked.length === ids.length) {
    filterSets[sec.filterKey] = null;
  } else {
    filterSets[sec.filterKey] = new Set(checked);
  }
  saveFilterSet(sec.filterKey);
  trigger.firstChild.textContent = filterDisplayLabel(sec.filterKey);
  trigger.title = filterDisplayLabel(sec.filterKey);
  renderAllCharts();
}

function getCollapsedSet() {
  try { return new Set(JSON.parse(localStorage.getItem('dfee-collapsed') || '[]')); }
  catch (_) { return new Set(); }
}
function saveCollapsedSet(set) {
  localStorage.setItem('dfee-collapsed', JSON.stringify([...set]));
}
function toggleSection(section, title) {
  const collapsed = section.classList.toggle('collapsed');
  const set = getCollapsedSet();
  if (collapsed) set.add(title); else set.delete(title);
  saveCollapsedSet(set);
  if (!collapsed) renderAllCharts();
}

// ---- helpers ----
function el(tag, cls) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  return e;
}
function elText(tag, cls, text) {
  const e = el(tag, cls);
  e.textContent = text;
  return e;
}
function fmt(v) {
  if (v === null || v === undefined) return '-';
  if (Number.isInteger(v)) return String(v);
  return Number(v).toFixed(2);
}
function fmtAxis(v) {
  if (v === 0) return '0';
  const abs = Math.abs(v);
  if (abs >= 1e12) return (v / 1e12).toFixed(1) + 'T';
  if (abs >= 1e9) return (v / 1e9).toFixed(1) + 'G';
  if (abs >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (abs >= 1e3) return (v / 1e3).toFixed(1) + 'K';
  return fmt(v);
}
function canvasHeight(seriesCount) {
  if (seriesCount === 0) return 0;
  return 200;
}
function showBanner(msg, isError) {
  const b = document.getElementById('banner');
  b.textContent = msg;
  b.classList.remove('hidden');
  b.style.background = isError ? '#fee2e2' : '#dcfce7';
  b.style.color = isError ? '#991b1b' : '#166534';
}
function hideBanner() {
  document.getElementById('banner').classList.add('hidden');
}

// ---- build sections ----
function buildSections(charts) {
  const container = document.getElementById('sections');
  container.innerHTML = '';
  canvasMap = {};
  legendMap = {};
  badgeMap = {};
  chartDefs = {};
  for (const c of charts) chartDefs[c.id] = c;

  for (const sec of SECTIONS) {
    const orderedIds = getOrderedIds(sec);
    const secCharts = orderedIds.map(id => chartDefs[id]).filter(c => c);
    const available = secCharts.filter(c => (c.series || []).length > 0).length;
    const hasPriority = secCharts.some(c => c.priority);
    const collapsedSet = getCollapsedSet();

    const section = el('section', 'section');
    section.style.setProperty('--section-accent', sec.accent);
    if (collapsedSet.has(sec.title)) section.classList.add('collapsed');

    const head = el('div', 'section-head');
    head.appendChild(elText('span', 'toggle-icon', '\u25BC'));
    head.appendChild(elText('span', '', sec.title));
    head.appendChild(elText('span', 'count', available + '/' + secCharts.length + ' 可用'));
    head.onclick = () => toggleSection(section, sec.title);
    if (hasPriority) {
      const filter = el('div', 'priority-filter');
      for (const [label, val] of [['全部',''], ['高','high'], ['中','medium'], ['低','low']]) {
        const btn = el('button', 'prio-btn');
        btn.textContent = label;
        btn.dataset.priority = val;
        if (val === '') btn.classList.add('active');
        btn.onclick = (e) => { e.stopPropagation(); togglePriority(grid, secCharts, val, btn, filter); };
        filter.appendChild(btn);
      }
      head.appendChild(filter);
    }
    if (sec.filterKey) {
      const filterIds = getFilterIds(secCharts);
      if (filterIds.length > 1) {
        head.appendChild(buildFilterDropdown(sec, filterIds));
      }
    }
    section.appendChild(head);

    const grid = el('div', 'chart-grid');
    grid.dataset.sectionTitle = sec.title;
    if (sec.gridCols) grid.style.gridTemplateColumns = `repeat(${sec.gridCols}, 1fr)`;
    secCharts.forEach((c) => {
      const span = sec.spans ? sec.spans[c.id] : undefined;
      grid.appendChild(buildCard(c, span));
    });
    section.appendChild(grid);
    container.appendChild(section);
  }
}

function togglePriority(grid, secCharts, val, clickedBtn, filterContainer) {
  const btns = filterContainer.querySelectorAll('.prio-btn');
  if (val === '') {
    // "全部" = show all, deactivate others
    btns.forEach(b => b.classList.remove('active'));
    clickedBtn.classList.add('active');
    secCharts.forEach((c, i) => {
      const card = grid.children[i];
      if (card) card.style.display = '';
    });
  } else {
    // Multi-select: toggle the clicked button
    clickedBtn.classList.toggle('active');
    // Deactivate "全部" if any specific button is active
    const allBtn = filterContainer.querySelector('.prio-btn[data-priority=""]');
    if (allBtn) allBtn.classList.remove('active');
    // Collect active priorities
    const active = [];
    btns.forEach(b => { if (b.classList.contains('active') && b.dataset.priority) active.push(b.dataset.priority); });
    // If nothing active, fall back to "全部"
    if (active.length === 0) {
      if (allBtn) allBtn.classList.add('active');
      secCharts.forEach((c, i) => { const card = grid.children[i]; if (card) card.style.display = ''; });
      return;
    }
    // Show/hide cards by priority
    secCharts.forEach((c, i) => {
      const card = grid.children[i];
      if (!card) return;
      card.style.display = active.includes(c.priority) ? '' : 'none';
    });
  }
}

function buildCard(chart, defaultSpan) {
  const series = chart.series || [];
  const hasData = series.length > 0;
  const bufferedCount = series.filter(s => buffers[s.id] && buffers[s.id].length > 0).length;
  const pending = hasData && bufferedCount === 0;

  const card = el('div', 'chart-card');
  card.dataset.chartId = chart.id;
  card.dataset.defaultSpan = defaultSpan || 1;
  if (!hasData) card.classList.add('compact');
  const savedSpan = cardSizes[chart.id]?.span || defaultSpan || 1;
  if (savedSpan > 1) card.style.gridColumn = `span ${savedSpan}`;

  // header (draggable for reorder)
  const head = el('div', 'chart-head');
  head.draggable = true;
  head.addEventListener('dragstart', (e) => {
    dragSource = card;
    card.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', chart.id);
  });
  head.addEventListener('dragend', () => {
    card.classList.remove('dragging');
    dragSource = null;
  });
  card.addEventListener('dragover', (e) => {
    e.preventDefault();
    if (dragSource && dragSource !== card) card.classList.add('drag-over');
  });
  card.addEventListener('dragleave', () => card.classList.remove('drag-over'));
  card.addEventListener('drop', (e) => {
    e.preventDefault();
    card.classList.remove('drag-over');
    if (!dragSource || dragSource === card) return;
    const grid = card.parentElement;
    const srcIdx = [...grid.children].indexOf(dragSource);
    const tgtIdx = [...grid.children].indexOf(card);
    if (srcIdx < tgtIdx) grid.insertBefore(dragSource, card.nextSibling);
    else grid.insertBefore(dragSource, card);
    const secTitle = grid.dataset.sectionTitle;
    cardOrders[secTitle] = [...grid.children].map(c => c.dataset.chartId);
    saveCardOrder();
    renderAllCharts();
  });

  const titleText = chart.y_unit ? chart.title + ' (' + chart.y_unit + ')' : chart.title;
  head.appendChild(elText('span', '', titleText));
  let badge;
  if (!hasData) {
    badge = elText('span', 'badge badge-empty', '无数据');
  } else if (pending) {
    badge = elText('span', 'badge badge-pending', '采集中');
  } else {
    badge = elText('span', 'badge badge-ok', series.length + ' 条');
  }
  head.appendChild(badge);
  badgeMap[chart.id] = badge;
  card.appendChild(head);

  if (!hasData) return card;

  // legend (HTML)
  const legend = el('div', 'legend');
  legend.id = 'legend-' + chart.id;
  card.appendChild(legend);
  legendMap[chart.id] = legend;

  // canvas
  const body = el('div', 'chart-body');
  const canvas = document.createElement('canvas');
  canvas.className = 'chart-canvas';
  canvas.id = 'canvas-' + chart.id;
  canvas.style.height = (cardSizes[chart.id]?.height || 200) + 'px';
  body.appendChild(canvas);
  card.appendChild(body);
  canvasMap[chart.id] = canvas;

  // resize handle
  const handle = el('div', 'resize-handle');
  handle.addEventListener('mousedown', startResize);
  card.appendChild(handle);

  return card;
}

const SNAP_SHOW = 5;
const SNAP_ALIGN = 3;

function clearGuides(grid) {
  grid.querySelectorAll('.guide-line').forEach(g => g.remove());
}

function showGuides(grid, card) {
  clearGuides(grid);
  const cardBottom = card.offsetTop + card.offsetHeight;
  let snapY = null;
  let snapDist = SNAP_SHOW;
  for (const other of grid.children) {
    if (other === card || !other.classList || !other.classList.contains('chart-card')) continue;
    const edges = [
      ['bottom', other.offsetTop + other.offsetHeight],
      ['top', other.offsetTop],
    ];
    for (const [, y] of edges) {
      const dist = Math.abs(cardBottom - y);
      if (dist <= SNAP_SHOW) {
        const guide = el('div', 'guide-line');
        guide.style.top = y + 'px';
        grid.appendChild(guide);
        if (dist <= SNAP_ALIGN && dist < snapDist) {
          snapDist = dist;
          snapY = y;
        }
      }
    }
  }
  return snapY;
}

function startResize(e) {
  e.preventDefault();
  e.stopPropagation();
  const card = e.currentTarget.closest('.chart-card');
  const chartId = card.dataset.chartId;
  const canvas = canvasMap[chartId];
  const grid = card.parentElement;
  const gridCols = getComputedStyle(grid).gridTemplateColumns.split(' ').length;
  const startX = e.clientX;
  const startY = e.clientY;
  const startHeight = canvas ? canvas.clientHeight : 200;
  const startWidth = card.offsetWidth;
  const startSpan = cardSizes[chartId]?.span || parseInt(card.dataset.defaultSpan) || 1;
  const colWidth = startWidth / startSpan;
  let currentSpan = startSpan;

  function onMove(ev) {
    const dx = ev.clientX - startX;
    const dy = ev.clientY - startY;
    const newHeight = Math.max(120, Math.min(500, startHeight + dy));
    if (canvas) canvas.style.height = newHeight + 'px';
    const newSpan = Math.max(1, Math.min(gridCols, Math.round(startSpan + dx / colWidth)));
    if (newSpan !== currentSpan) {
      currentSpan = newSpan;
      card.style.gridColumn = newSpan > 1 ? `span ${newSpan}` : '';
    }
    if (canvas) renderChart(canvas, chartDefs[chartId]);
    const snapY = showGuides(grid, card);
    if (snapY !== null && canvas) {
      const currentBottom = card.offsetTop + card.offsetHeight;
      const delta = snapY - currentBottom;
      const snapped = Math.max(120, Math.min(500, newHeight + delta));
      canvas.style.height = snapped + 'px';
      renderChart(canvas, chartDefs[chartId]);
    }
  }
  function onUp() {
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
    clearGuides(grid);
    if (!cardSizes[chartId]) cardSizes[chartId] = {};
    cardSizes[chartId].span = currentSpan;
    cardSizes[chartId].height = parseInt(canvas?.style.height) || 200;
    saveCardSize();
    renderAllCharts();
  }
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}

// ---- buffer update ----
function updateBuffers(data) {
  const seen = {};
  for (const c of data.charts) {
    for (const s of (c.series || [])) {
      seen[s.id] = true;
      if (!buffers[s.id]) buffers[s.id] = [];
      buffers[s.id].push(s.value);
      if (buffers[s.id].length > HISTORY_POINTS) buffers[s.id].shift();
    }
  }
  for (const id in buffers) {
    if (!seen[id]) delete buffers[id];
  }
}

// ---- legend update (HTML) ----
function updateLegend(chart) {
  const legend = legendMap[chart.id];
  if (!legend) return;
  const series = (chart.series || []).filter(s => buffers[s.id] && buffers[s.id].length > 0 && isSeriesVisible(chart, s));
  legend.innerHTML = '';
  for (let i = 0; i < series.length; i++) {
    const s = series[i];
    const color = PALETTE[i % PALETTE.length];
    const val = buffers[s.id][buffers[s.id].length - 1];
    const unit = s.unit ? ' ' + s.unit : '';

    const item = el('span', 'legend-item');
    const dot = el('span', 'legend-dot');
    dot.style.background = color;
    item.appendChild(dot);
    item.appendChild(document.createTextNode(s.label));
    const valSpan = el('span', 'legend-val');
    valSpan.textContent = ' ' + fmt(val) + unit;
    item.appendChild(valSpan);
    legend.appendChild(item);
  }
}

// ---- canvas rendering ----
function renderChart(canvas, chart) {
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio || 1;
  const cw = canvas.clientWidth;
  const ch = canvas.clientHeight;
  if (cw === 0 || ch === 0) return;
  canvas.width = cw * dpr;
  canvas.height = ch * dpr;
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, cw, ch);

  const series = (chart.series || []).filter(s => buffers[s.id] && buffers[s.id].length > 0 && isSeriesVisible(chart, s));
  if (series.length === 0) return;

  // Y axis range
  let min = Infinity, max = -Infinity;
  for (const s of series) {
    for (const v of buffers[s.id]) {
      if (v < min) min = v;
      if (v > max) max = v;
    }
  }
  if (min === max) max = min + 1;
  // When data is nearly flat (variation < 1% of magnitude), expand Y axis
  // to start from 0 so grid labels are distinguishable. This is common for
  // cumulative counters (e.g. network bytes_total) where deltas are tiny.
  if (min > 0 && (max - min) / max < 0.01) {
    min = 0;
  }

  const padL = 64, padR = 8, padT = 8, padB = 16;
  const plotW = cw - padL - padR;
  const plotH = ch - padT - padB;

  // grid + Y labels
  ctx.strokeStyle = '#f1f3f5';
  ctx.fillStyle = '#9ca3af';
  ctx.font = '10px sans-serif';
  ctx.textAlign = 'right';
  ctx.textBaseline = 'middle';
  const gridN = 4;
  for (let i = 0; i <= gridN; i++) {
    const y = padT + (plotH * i) / gridN;
    ctx.beginPath();
    ctx.moveTo(padL, y);
    ctx.lineTo(cw - padR, y);
    ctx.stroke();
    const val = max - (max - min) * (i / gridN);
    const label = fmtAxis(val) + (chart.y_unit ? ' ' + chart.y_unit : '');
    ctx.fillText(label, padL - 4, y);
  }

  // X axis — label reflects actual data span, not full capacity.
  const maxDataLen = Math.max(...series.map(s => buffers[s.id].length));
  const actualSpan = (refreshIntervalMs * maxDataLen) / 1000;
  let spanStr = actualSpan >= 3600 ? (actualSpan / 3600).toFixed(0) + 'h' : actualSpan >= 60 ? Math.round(actualSpan / 60) + 'min' : Math.round(actualSpan) + 's';
  ctx.textAlign = 'left';
  ctx.textBaseline = 'top';
  ctx.fillText('−' + spanStr, padL, padT + plotH + 2);
  ctx.textAlign = 'right';
  ctx.fillText('now', cw - padR, padT + plotH + 2);

  // polylines — right-aligned: most recent data at right edge, older data
  // grows leftward as the buffer fills. This prevents a 2-point line from
  // stretching across the full chart width.
  const denom = HISTORY_POINTS - 1;
  for (let si = 0; si < series.length; si++) {
    const s = series[si];
    const data = buffers[s.id];
    const n = data.length;
    if (n < 2) continue;
    ctx.strokeStyle = PALETTE[si % PALETTE.length];
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    for (let i = 0; i < n; i++) {
      const x = padL + plotW * (i + (HISTORY_POINTS - n)) / denom;
      const y = padT + plotH - ((data[i] - min) / (max - min)) * plotH;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    }
    ctx.stroke();
  }
}

function updateBadge(chart) {
  const badge = badgeMap[chart.id];
  if (!badge) return;
  const allSeries = chart.series || [];
  const visibleSeries = allSeries.filter(s => isSeriesVisible(chart, s));
  const hasData = allSeries.length > 0;
  const bufferedCount = visibleSeries.filter(s => buffers[s.id] && buffers[s.id].length > 0).length;
  if (!hasData) {
    badge.className = 'badge badge-empty';
    badge.textContent = '无数据';
  } else if (bufferedCount === 0) {
    badge.className = 'badge badge-pending';
    badge.textContent = '采集中';
  } else {
    badge.className = 'badge badge-ok';
    badge.textContent = visibleSeries.length + ' 条';
  }
}

function renderAllCharts() {
  for (const c of Object.values(chartDefs)) {
    updateBadge(c);
    updateLegend(c);
    const canvas = canvasMap[c.id];
    if (canvas) renderChart(canvas, c);
  }
}

// ---- data fetching ----
async function fetchData() {
  try {
    const r = await fetch('/api/dfee', { cache: 'no-store' });
    if (!r.ok) { showBanner('能效快照尚未就绪，等待首次采集…', true); return null; }
    hideBanner();
    return await r.json();
  } catch (e) {
    showBanner('获取数据失败：' + e.message, true);
    return null;
  }
}

async function pollTick() {
  const data = await fetchData();
  if (!data) return;
  refreshIntervalMs = data.refresh_interval_ms || refreshIntervalMs;
  document.getElementById('intervalDisplay').textContent = (refreshIntervalMs / 1000) + 's';
  const ts = data.timestamp ? new Date(data.timestamp) : null;
  document.getElementById('updateTime').textContent = ts ? ts.toLocaleTimeString('zh-CN') : '--';

  // Detect server restart: if session_id changed, clear all cached layout.
  if (data.session_id) {
    const saved = localStorage.getItem('dfee-session-id');
    if (saved && saved !== data.session_id) {
      localStorage.removeItem('dfee-card-order');
      localStorage.removeItem('dfee-card-size');
      localStorage.removeItem('dfee-collapsed');
      for (const sec of SECTIONS) {
        if (sec.filterKey) localStorage.removeItem('dfee-filter-' + sec.filterKey);
      }
      cardOrders = {};
      cardSizes = {};
      filterSets = {};
    }
    localStorage.setItem('dfee-session-id', data.session_id);
  }

  updateBuffers(data);

  if (Object.keys(chartDefs).length === 0 && data.charts) {
    buildSections(data.charts);
  } else if (data.charts) {
    // Rebuild if chart IDs changed OR any chart's series went from empty to
    // non-empty (or vice versa). Without this, a chart that was compact on
    // first poll (e.g. IOPS needs prev snapshot) would never get a canvas.
    const oldKeys = Object.keys(chartDefs).sort().join(',');
    const newKeys = data.charts.map(c => c.id).sort().join(',');
    let needRebuild = oldKeys !== newKeys;
    if (!needRebuild) {
      for (const c of data.charts) {
        const old = chartDefs[c.id];
        const oldHas = old && (old.series || []).length > 0;
        const newHas = (c.series || []).length > 0;
        if (!oldHas && newHas) { needRebuild = true; break; }
      }
    }
    if (needRebuild) { buildSections(data.charts); }
    else { data.charts.forEach(c => { chartDefs[c.id] = c; }); }
  }
  renderAllCharts();
}

function startPolling() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(pollTick, refreshIntervalMs);
  pollTick();
}

async function manualRefresh() {
  try { await fetch('/api/refresh', { method: 'POST' }); } catch (e) { /* ignore */ }
  setTimeout(pollTick, 400);
}

// ---- init ----
document.getElementById('refreshBtn').addEventListener('click', manualRefresh);
(async function init() {
  loadCardLayout();
  for (const sec of SECTIONS) {
    if (sec.filterKey) loadFilterSet(sec.filterKey);
  }
  await pollTick();
  startPolling();
})();

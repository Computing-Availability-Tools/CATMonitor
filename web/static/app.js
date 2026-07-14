// ---- display manifest (optional hints; unknown components render generically) ----
const MANIFEST = {
  cpu: { title: 'CPU', headline: 'cpu_usage', headlineLabel: 'CPU 使用率 (%)',
         key: [ {name:'usage', prefer:{core:'total'}}, 'load_average', 'avg_freq',
                'temperature', 'power', 'cpu_ce_errors', 'model_info' ] },
  memory: { title: '内存', headline: 'memory_usage', headlineLabel: '内存使用率 (%)',
            key: [ 'usage', 'swap_usage', 'saturation', 'fragmentation',
                   'module_num', 'ecc_ce_errors', 'oom_count', 'page_faults' ] },
  disk: { title: '磁盘', headline: 'disk_space_usage', headlineLabel: '磁盘使用率 (%)',
          key: [ 'space_usage', 'throughput', 'iops', 'io_wait',
                 'io_errors', 'smart_status' ] },
  gpu: { title: 'GPU', headline: 'gpu_utilization', headlineLabel: 'GPU 使用率 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  npu: { title: 'NPU', headline: 'npu_utilization', headlineLabel: 'NPU 使用率 (%)',
         key: [ 'utilization', 'memory_usage', 'temperature', 'power_draw' ] },
  network: { title: '网络', headline: null,
             key: [ 'throughput', 'packet_count', 'error_count', 'connection_count' ] },
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
  // v0.2.0 CPU source-layer metrics.
  user_time: '用户时间', nice_time: 'Nice 时间', system_time: '系统时间',
  idle_time: '空闲时间', iowait_time: 'IO 等待时间', irq_time: '中断时间',
  softirq_time: '软中断时间', steal_time: '抢占时间',
  user_util: '用户使用率', system_util: '系统使用率', idle_util: '空闲率', iowait_util: 'IO 等待率',
  avg_freq: '平均频率', min_freq: '最小频率', max_freq: '最大频率',
  numa_node_num: 'NUMA 节点数', core_num: '物理核数', die_core_num: 'Die 核数',
  numa_core_num: 'NUMA 核数', cpu_num: 'CPU 数',
  online_core_num: '在线核数', offline_core_num: '离线核数', isolated_core_num: '隔离核数',
  l1d_cache_size: 'L1d 缓存', l1i_cache_size: 'L1i 缓存', l2_cache_size: 'L2 缓存', l3_cache_size: 'L3 缓存',
  numa_order_num: 'NUMA 阶数', numa_info: 'NUMA 最高阶',
  cpu_ce_errors: 'CPU CE 错误', cpu_uce_errors: 'CPU UCE 错误',
  mem_temperature: '内存温度', power: '功耗',
  // v0.2.0 Memory source-layer metrics.
  swap_in: 'Swap 入', swap_out: 'Swap 出',
  saturation: '内存压力', fragmentation: '碎片化',
  isolated_pages: '隔离页', isolated_anon_pages: '隔离匿名页',
  isolated_file_pages: '隔离文件页', free_pages: '空闲页',
  module_size: '内存条容量', module_info: '内存条信息', module_num: '内存条数',
  ecc_ce_errors: 'ECC CE 错误', ecc_uce_errors: 'ECC UCE 错误',
  // Hardware identity (system collector, one-shot).
  device_model: '设备型号', gpu_info: 'GPU 信息', npu_info: 'NPU 信息',
  disk_info: '磁盘信息', net_info: '网卡信息',
};

// Label key -> Chinese display name, for the detail-page hardware-specs panel.
const LABEL_NAMES = {
  manufacturer: '厂商', product_name: '型号', version: '版本', serial_number: '序列号',
  name: '名称', uuid: 'UUID', driver_version: '驱动版本', bus_id: '总线',
  model: '型号', serial: '序列号', size: '容量', interface: '接口', firmware: '固件',
  mac: 'MAC', mtu: 'MTU', speed: '速率', driver: '驱动',
  locator: '插槽', type: '类型',
  model_name: '型号', cache_size: '缓存', core: '核心', node: '节点', die: 'Die',
};

// Maps a static spec metric name to (display type, the label key that holds
// the primary identity shown in the "标识" column of the specs panel + the
// overview card spec summary.
const SPEC_DEFS = {
  device_model: { type: '设备', primary: 'product_name' },
  model_info:   { type: 'CPU', primary: 'model_name' },
  gpu_info:     { type: 'GPU', primary: 'name' },
  npu_info:     { type: 'NPU', primary: 'name' },
  disk_info:    { type: '磁盘', primary: 'model' },
  net_info:     { type: '网卡', primary: 'interface' },
  module_info:  { type: '内存条', primary: 'locator' },
};

const SERIES_LABELS = {
  cpu_usage: 'CPU 使用率 (%)', cpu_load_average: '负载 (1m)',
  memory_usage: '内存使用率 (%)', memory_swap_usage: 'Swap 使用率 (%)',
  disk_space_usage: '磁盘使用率 (%)',
  gpu_utilization: 'GPU 使用率 (%)', gpu_memory_usage: 'GPU 显存使用率 (%)', gpu_temperature: 'GPU 温度 (°C)',
  npu_utilization: 'NPU 使用率 (%)', npu_memory_usage: 'NPU 显存使用率 (%)', npu_temperature: 'NPU 温度 (°C)',
  // v0.2.0 trends.
  cpu_temperature: 'CPU 温度 (°C)', cpu_power: 'CPU 功耗 (W)',
  cpu_avg_freq: 'CPU 平均频率 (MHz)', cpu_context_switches: '上下文切换 (次/s)',
  cpu_ce_errors: 'CPU CE 错误 (次)',
  memory_saturation: '内存压力 (%)', memory_fragmentation: '内存碎片化 (%)',
  memory_swap_in: 'Swap 入页 (次/s)', memory_power: '内存功耗 (W)',
  disk_io_wait: 'IO Wait (%)', disk_iops: '磁盘 IOPS (次/s)', disk_throughput: '磁盘吞吐 (MB/s)',
  network_throughput: '网络吞吐 (bytes/s)', network_packet_count: '网络包速率 (个/s)',
  network_error_count: '网络错误 (次)',
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

// statBounds computes min/max/mean of a series. max is nudged above min so a
// flat series still renders (avoids divide-by-zero).
function statBounds(series) {
  let min = Infinity, max = -Infinity, sum = 0;
  for (const v of series) { if (v < min) min = v; if (v > max) max = v; sum += v; }
  if (max === min) max = min + 1;
  return { min, max, mean: sum / series.length };
}

// meanLine returns a horizontal dashed SVG line at viewBox y, used to mark the
// series average on both the small sparkline and the detail chart.
function meanLine(y) {
  const l = document.createElementNS('http://www.w3.org/2000/svg', 'line');
  l.setAttribute('x1', 0); l.setAttribute('x2', 100);
  l.setAttribute('y1', y); l.setAttribute('y2', y);
  l.setAttribute('stroke', '#9ca3af');
  l.setAttribute('stroke-width', '1');
  l.setAttribute('stroke-dasharray', '3,2');
  l.setAttribute('vector-effect', 'non-scaling-stroke');
  return l;
}

// sparkline renders a compact preview polyline (overview card headline).
// Includes the mean dashed line; no axis labels (too small for ticks there).
function sparkline(series, color) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 100 30');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.classList.add('spark');
  const n = series.length;
  if (n < 2) return svg;
  const { min, max, mean } = statBounds(series);
  const yOf = v => 28 - ((v - min) / (max - min)) * 24 - 2;
  svg.appendChild(meanLine(yOf(mean)));
  const pts = series.map((v, i) => ((i / (n - 1)) * 100).toFixed(2) + ',' + yOf(v).toFixed(2)).join(' ');
  const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  poly.setAttribute('points', pts);
  poly.setAttribute('fill', 'none');
  poly.setAttribute('stroke', color);
  poly.setAttribute('stroke-width', '1.5');
  poly.setAttribute('vector-effect', 'non-scaling-stroke');
  svg.appendChild(poly);
  return svg;
}

// chartTimeSpan formats the X-axis window: history_points × refresh interval.
function chartTimeSpan(snap) {
  const pts = snap.history_points || 60;
  const s = ((snap.refresh_interval_ms || 5000) * pts) / 1000;
  if (s >= 3600) return (s / 3600).toFixed(0) + ' 小时';
  if (s >= 60) return Math.round(s / 60) + ' 分钟';
  return Math.round(s) + ' 秒';
}

// renderChart renders the detail-page trend chart: Y axis (min/max labels), the
// data polyline + a mean dashed line, and an X axis (time span -> now).
function renderChart(series, color, snap) {
  const n = series.length;
  const chart = el('div', 'chart');
  if (n < 2) { chart.appendChild(elText('div', 'empty', '数据不足')); return chart; }
  const { min, max, mean } = statBounds(series);
  const yOf = v => 28 - ((v - min) / (max - min)) * 24 - 2;

  const row = el('div', 'chart-row');
  const ycol = el('div', 'chart-y');
  ycol.appendChild(elText('span', 'axis-val', fmt(max)));
  ycol.appendChild(elText('span', 'axis-val', fmt(min)));

  const plot = el('div', 'chart-plot');
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 100 30');
  svg.setAttribute('preserveAspectRatio', 'none');
  svg.classList.add('spark');
  svg.appendChild(meanLine(yOf(mean)));
  const pts = series.map((v, i) => ((i / (n - 1)) * 100).toFixed(2) + ',' + yOf(v).toFixed(2)).join(' ');
  const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  poly.setAttribute('points', pts);
  poly.setAttribute('fill', 'none');
  poly.setAttribute('stroke', color);
  poly.setAttribute('stroke-width', '1.5');
  poly.setAttribute('vector-effect', 'non-scaling-stroke');
  svg.appendChild(poly);
  plot.appendChild(svg);
  // Mean value label sitting on the dashed line: the line's viewBox y maps to
  // (y/30) of the stretched plot height, so position the badge at that %.
  const badge = elText('span', 'mean-badge', '均值 ' + fmt(mean));
  badge.style.top = (yOf(mean) / 30 * 100) + '%';
  plot.appendChild(badge);
  row.appendChild(ycol);
  row.appendChild(plot);
  chart.appendChild(row);

  const xrow = el('div', 'chart-x');
  xrow.appendChild(elText('span', 'axis-val', '−' + chartTimeSpan(snap)));
  xrow.appendChild(elText('span', 'axis-val', '现在'));
  chart.appendChild(xrow);
  return chart;
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
  // The "system" collector emits static identity metrics (device_model etc.
  // attributed to gpu/npu/disk/network) but has no dynamic page of its own, so
  // hide it from the nav and overview grid.
  return collectors.filter(c => c.component !== 'system').slice().sort((a, b) => {
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

// specSummary returns a one-line identity string for a component's overview
// card (e.g. "Tesla T4 等 2 卡", "Samsung 970 等 2 块", "eth0 1000Mb/s").
// Reads stashed static specs (snap.specs); empty string when none available.
// ---- specs helpers ----
// specVal finds the first static spec with the given name (and optional
// component) in snap.specs.
function specVal(specs, name, comp) {
  for (const m of specs) {
    if (m.name === name && (comp === undefined || m.component === comp)) return m;
  }
  return null;
}
// memoryTotalMB returns the total physical memory in MB, read from the
// every-cycle usage_detail metric (works on Linux + Windows without root).
function memoryTotalMB(snap) {
  const m = pickMetric(snap.metrics || [], { name: 'usage_detail', prefer: { field: 'total' } });
  return m ? m.value : 0;
}
function fmtGB(gb) {
  if (gb >= 1024) return (gb / 1024).toFixed(1) + ' TB';
  return gb.toFixed(gb >= 10 ? 0 : 1) + ' GB';
}
function fmtMB(mb) { return fmtGB(mb / 1024); }
function netStr(m) {
  const lb = m.labels || {};
  const sfx = lb.speed && lb.speed !== '-1' ? ' ' + lb.speed + 'Mb/s' : '';
  return (lb.interface || '') + sfx;
}

// ---- specs panel (top-right of overview hero) ----
// Compact: 1-2 core static specs per component. Click opens a modal with the
// full static hardware info grouped by component.
function renderSpecs(snap) {
  const specs = snap.specs || [];
  const panel = el('div', 'specs clickable');
  panel.onclick = () => openSpecsModal(snap);
  panel.appendChild(elText('div', 'specs-title', '设备规格'));
  const kv = el('div', 'specs-kv');
  const add = (k, v) => {
    if (!v) return;
    kv.appendChild(elText('div', 'k', k));
    kv.appendChild(elText('div', 'v', v));
  };

  const dev = specVal(specs, 'device_model');
  if (dev) add('设备', [dev.labels.manufacturer, dev.labels.product_name].filter(x => x).join(' '));

  const mi = specVal(specs, 'model_info');
  if (mi) add('CPU', (mi.labels || {}).model_name || '');

  const mt = memoryTotalMB(snap);
  if (mt) add('内存', fmtMB(mt));

  const disks = specs.filter(m => m.name === 'disk_info');
  if (disks.length) add('硬盘', disks.length + ' 块, 共 ' + fmtGB(disks.reduce((s, m) => s + m.value, 0)));

  const nets = specs.filter(m => m.name === 'net_info');
  if (nets.length) add('网卡', nets.slice(0, 2).map(netStr).join(', ') + (nets.length > 2 ? ' …' : ''));

  const gpus = specs.filter(m => m.name === 'gpu_info');
  if (gpus.length) add('GPU', (gpus[0].labels || {}).name + (gpus.length > 1 ? ' 等 ' + gpus.length + ' 卡' : ''));
  const npus = specs.filter(m => m.name === 'npu_info');
  if (npus.length) add('NPU', (npus[0].labels || {}).name + (npus.length > 1 ? ' 等 ' + npus.length + ' 卡' : ''));

  if (!kv.children.length) {
    panel.appendChild(elText('div', 'empty', '无静态规格信息'));
  } else {
    panel.appendChild(kv);
    panel.appendChild(elText('div', 'specs-hint', '点击查看完整规格 ▸'));
  }
  return panel;
}

// ---- specs modal (full static hardware info) ----
function openSpecsModal(snap) {
  const old = document.getElementById('specsModal');
  if (old) old.remove();
  const specs = snap.specs || [];
  const overlay = el('div', 'modal-overlay');
  overlay.id = 'specsModal';
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove(); };

  const card = el('div', 'modal-card');
  const head = el('div', 'modal-head');
  head.appendChild(elText('span', '', '设备规格 · 完整信息'));
  const close = el('button', 'modal-close');
  close.textContent = '×';
  close.onclick = () => overlay.remove();
  head.appendChild(close);
  card.appendChild(head);

  const body = el('div', 'modal-body');
  const groups = {};
  for (const m of specs) { (groups[m.component] = groups[m.component] || []).push(m); }

  // Memory total comes from the every-cycle usage_detail metric, not specs;
  // inject it so the memory group is never empty on hosts without dmidecode.
  const mt = memoryTotalMB(snap);
  if (mt) {
    groups['memory'] = (groups['memory'] || []).concat([{
      component: 'memory', name: 'mem_total', value: mt,
      labels: { capacity: fmtMB(mt) }, synthetic: true,
    }]);
  }

  const order = ['system', 'cpu', 'memory', 'disk', 'gpu', 'npu', 'network'];
  const seen = {};
  for (const comp of order) {
    seen[comp] = true;
    if (groups[comp] && groups[comp].length) body.appendChild(specsGroup(comp, groups[comp]));
  }
  for (const comp in groups) {
    if (!seen[comp] && groups[comp].length) body.appendChild(specsGroup(comp, groups[comp]));
  }
  if (!body.children.length) body.appendChild(elText('div', 'empty', '无静态规格信息'));
  card.appendChild(body);
  overlay.appendChild(card);
  document.body.appendChild(overlay);
}

// specsGroup renders one component's static specs as a titled table
// (类型 / 标识 / 明细). Synthetic entries (mem_total) get a friendly type.
function specsGroup(comp, arr) {
  const sec = el('div', 'specs-group');
  const title = comp === 'system' ? '设备' : compTitle(comp);
  sec.appendChild(elText('div', 'specs-group-title', title + ' (' + arr.length + ')'));
  const tbl = document.createElement('table');
  tbl.className = 'table';
  tbl.innerHTML = '<thead><tr><th>类型</th><th>标识</th><th>明细</th></tr></thead>';
  const tb = document.createElement('tbody');
  for (const m of arr) {
    const def = SPEC_DEFS[m.name] || { type: (METRIC_NAMES[m.name] || m.name), primary: '' };
    const lb = m.labels || {};
    const primary = def.primary ? (lb[def.primary] || '') : (m.synthetic ? fmtMB(m.value) : '');
    const rest = [];
    for (const k in lb) {
      if (k === def.primary) continue;
      rest.push((LABEL_NAMES[k] || k) + ': ' + lb[k]);
    }
    const tr = document.createElement('tr');
    tr.innerHTML =
      '<td class="m-name">' + (m.synthetic ? '内存容量' : def.type) + '</td>' +
      '<td class="m-val">' + primary + '</td>' +
      '<td class="m-labels">' + rest.join(', ') + '</td>';
    tb.appendChild(tr);
  }
  tbl.appendChild(tb);
  sec.appendChild(tbl);
  return sec;
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
  hero.appendChild(renderSpecs(snap));
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
      item.appendChild(elText('div', 'tval', '当前 ' + fmt(cur)));
      item.appendChild(renderChart(s.data, st.color, snap));
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

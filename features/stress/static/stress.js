'use strict';

window.clearTimeout(window.__stressBootTimer);

let config = null;
let report = null;
let selected = null;
let historyReports = [];
let viewedJobID = '';
const expandedProfiles = new Set();

const byId = id => document.getElementById(id);
const benchmarkInfo = {
  stream: {description: '验证持续内存带宽与数据搬运能力', family: 'Memory bandwidth'},
  hpl: {description: '验证高密度浮点计算与 MPI 运行能力', family: 'Dense compute'},
  hpcg: {description: '验证内存、计算与通信的综合可靠性', family: 'System workload'}
};
const statusInfo = {
  healthy: {label: '通过', cls: 'ok'},
  time_limit_reached: {label: '通过 · 达到时限', cls: 'ok'},
  running: {label: '运行中', cls: 'running'},
  pending: {label: '等待中', cls: 'running'},
  cancelled: {label: '已取消', cls: 'bad'},
  unavailable: {label: '不可用', cls: 'bad'},
  unsupported: {label: '不支持', cls: 'bad'},
  timeout: {label: '未完成', cls: 'bad'},
  unhealthy: {label: '失败', cls: 'bad'}
};
const metricInfo = {
  copy_mb_s: {label: 'Copy', unit: 'MB/s'},
  scale_mb_s: {label: 'Scale', unit: 'MB/s'},
  add_mb_s: {label: 'Add', unit: 'MB/s'},
  triad_mb_s: {label: 'Triad', unit: 'MB/s'},
  gflops: {label: '浮点性能', unit: 'GFLOP/s'},
  time_seconds: {label: '基准计算时间', unit: 's'},
  n: {label: '问题规模 N', unit: ''},
  nb: {label: '块大小 NB', unit: ''},
  process: {label: 'MPI 进程', unit: ''}
};
const streamMetricOrder = ['copy_mb_s', 'scale_mb_s', 'add_mb_s', 'triad_mb_s'];
const preflightLabels = {
  pass: '预检通过',
  warn: '预检有警告',
  fail: '预检失败',
  unsupported: '旧版脚本'
};

function visual(status) {
  return statusInfo[status] || {label: status || '未运行', cls: 'neutral'};
}
function setError(message) { byId('error').textContent = message || ''; }
function duration(ms) {
  if (typeof ms !== 'number' || ms < 0) return '--';
  if (ms < 1000) return ms + ' ms';
  const seconds = ms / 1000;
  return seconds < 60 ? seconds.toFixed(2) + ' s' : (seconds / 60).toFixed(2) + ' min';
}
function sourceLabel(source) {
  if (source === 'cli') return '宿主机 CLI';
  if (source === 'web') return 'Web';
  return source || '--';
}
function jobDuration(value) {
  if (!value || !value.started_at) return '--';
  const end = value.finished_at ? new Date(value.finished_at) : new Date();
  return duration(Math.max(0, end - new Date(value.started_at)));
}
function activeBenchmark() {
  if (!report || !report.benchmarks) return '';
  const active = report.benchmarks.find(item => item.status === 'running');
  if (active) return active.name.toUpperCase();
  const pending = report.benchmarks.find(item => item.status === 'pending');
  return pending ? pending.name.toUpperCase() : '';
}
function metricWidth(value, values) {
  const numbers = (values || []).filter(item => Number.isFinite(item) && item >= 0);
  const maximum = Math.max(...numbers, 1);
  return Math.max(8, Math.min(100, Number(value) / maximum * 100));
}
function metricValue(value) {
  if (!Number.isFinite(value)) return String(value);
  if (Math.abs(value) >= 1000) return value.toLocaleString('zh-CN', {maximumFractionDigits: 2});
  return value.toLocaleString('zh-CN', {maximumFractionDigits: 3});
}
function metricText(value, unit) {
  const rendered = metricValue(value);
  return unit ? rendered + ' ' + unit : rendered;
}
function shortHash(value) {
  return value ? value.slice(0, 12) : '--';
}
function profileValue(value, fallback) {
  return value === undefined || value === null || value === '' ? (fallback || '--') : String(value);
}
function appendResource(container, label, value) {
  const item = document.createElement('div');
  item.className = 'resource-item';
  const key = document.createElement('span');
  key.textContent = label;
  const rendered = document.createElement('b');
  rendered.textContent = profileValue(value);
  item.append(key, rendered);
  container.appendChild(item);
}
function appendProfileParameters(container, profile) {
  const parameters = document.createElement('div');
  parameters.className = 'profile-parameters';
  for (const item of profile.parameters || []) {
    const row = document.createElement('div');
    row.className = 'profile-parameter';
    const label = document.createElement('span');
    label.textContent = item.label;
    const value = document.createElement('b');
    value.textContent = profileValue(item.value, '未检测') + (item.unit ? ' ' + item.unit : '');
    row.append(label, value);
    parameters.appendChild(row);
  }
  container.appendChild(parameters);
}
function renderProfileBody(container, profile, timeoutOverride) {
  const resources = profile.resources || {};
  const effectiveTimeout = Number.isInteger(timeoutOverride) && timeoutOverride > 0 &&
    timeoutOverride <= profile.timeout_seconds
    ? timeoutOverride
    : profile.timeout_seconds;
  const strip = document.createElement('div');
  strip.className = 'resource-strip';
  appendResource(strip, '作业时限', effectiveTimeout + ' s');
  appendResource(strip, '总工作单元', resources.total_workers > 0 ? resources.total_workers : '自动');
  appendResource(strip, 'MPI 进程', resources.mpi_processes > 0 ? resources.mpi_processes : '不使用');
  appendResource(strip, '问题规模', resources.problem_size || '由执行器决定');
  container.appendChild(strip);
  appendProfileParameters(container, profile);

  const checks = document.createElement('div');
  checks.className = 'profile-checks';
  for (const asset of profile.assets || []) {
    const check = document.createElement('span');
    check.className = 'profile-check ' + asset.status;
    check.title = asset.path + ' · ' + asset.message;
    check.textContent = asset.name + ' · ' + (asset.status === 'pass' ? '可用' : '失败');
    checks.appendChild(check);
  }
  if (profile.mpi && profile.mpi.required) {
    const mpi = document.createElement('span');
    mpi.className = 'profile-check ' + profile.mpi.status;
    mpi.title = profile.mpi.message;
    mpi.textContent = 'MPI ' + profileValue(profile.mpi.implementation, 'unknown') +
      ' / ABI ' + profileValue(profile.mpi.executable_abi, 'unknown');
    checks.appendChild(mpi);
  }
  container.appendChild(checks);

  const hash = document.createElement('div');
  hash.className = 'profile-hash';
  hash.textContent = timeoutOverride > 0 && effectiveTimeout === timeoutOverride
    ? '配置 SHA-256 · 提交后按单次时限生成'
    : '配置 SHA-256 · ' + profileValue(profile.configuration_sha256);
  container.appendChild(hash);
  if (profile.preflight && profile.preflight.status !== 'pass') {
    const warning = document.createElement('div');
    warning.className = 'profile-warning';
    warning.textContent = profile.preflight.message;
    container.appendChild(warning);
  }
}
function renderProfilePreview() {
  const preview = byId('profile-preview');
  const cards = byId('profile-cards');
  cards.innerHTML = '';
  if (!config || !selected || selected.size === 0) {
    preview.hidden = true;
    return;
  }
  const items = (config.benchmarks || []).filter(item => selected.has(item.name));
  const rawTimeout = byId('timeout').value.trim();
  const timeoutOverride = rawTimeout ? Number(rawTimeout) : 0;
  preview.hidden = items.length === 0;
  for (const item of items) {
    const card = document.createElement('article');
    card.className = 'profile-card';
    const head = document.createElement('div');
    head.className = 'profile-card-head';
    const name = document.createElement('span');
    name.className = 'profile-card-name';
    name.textContent = item.name;
    const preflight = document.createElement('span');
    const status = item.profile && item.profile.preflight
      ? item.profile.preflight.status
      : 'unsupported';
    preflight.className = 'preflight-state ' + status;
    preflight.textContent = preflightLabels[status] || status;
    head.append(name, preflight);
    card.appendChild(head);
    if (item.profile) {
      renderProfileBody(card, item.profile, timeoutOverride);
    } else {
      const warning = document.createElement('div');
      warning.className = 'profile-warning';
      warning.textContent = item.profile_error || '执行参数不可用';
      card.appendChild(warning);
    }
    cards.appendChild(card);
  }
}
function reportsForNavigation() {
  const items = [];
  const seen = new Set();
  if (report && !historyReports.some(item => item.job_id === report.job_id)) {
    items.push(report);
    seen.add(report.job_id);
  }
  for (const item of historyReports) {
    if (!seen.has(item.job_id)) {
      items.push(item);
      seen.add(item.job_id);
    }
  }
  return items;
}
function displayedReport() {
  if (!viewedJobID) return report;
  return reportsForNavigation().find(item => item.job_id === viewedJobID) || report;
}
function benchmarkSeries(name, key) {
  const values = [];
  const seen = new Set();
  const candidates = reportsForNavigation().slice().sort((a, b) =>
    new Date(a.started_at) - new Date(b.started_at)
  );
  for (const candidate of candidates) {
    if (!candidate || seen.has(candidate.job_id) || candidate.status === 'running') continue;
    seen.add(candidate.job_id);
    const benchmark = (candidate.benchmarks || []).find(item => item.name === name);
    const value = benchmark && benchmark.values && benchmark.values[key];
    if (Number.isFinite(value)) values.push(value);
  }
  return values;
}

function renderSummary() {
  const status = visual(report && report.status);
  byId('overview-status').textContent = status.label;
  const benchmarks = (config && config.benchmarks) || [];
  const available = benchmarks.filter(item => item.enabled && item.available).length;
  byId('overview-available').textContent = available + ' / ' + benchmarks.length;
  byId('overview-source').textContent = sourceLabel(report && report.initiator);
  byId('overview-duration').textContent = jobDuration(report);
}

function renderConfig() {
  const choices = byId('choices');
  choices.innerHTML = '';
  if (!config) return;
  const selectable = (config.benchmarks || []).filter(item => item.enabled && item.available);
  if (selected === null) {
    selected = new Set((config.default_benchmarks || []).filter(name =>
      selectable.some(item => item.name === name)
    ));
  }
  const running = report && report.status === 'running';

  for (const item of config.benchmarks || []) {
    const available = item.enabled && item.available;
    const info = benchmarkInfo[item.name] || {
      description: '验证节点在高负载下的运行可靠性',
      family: 'Reliability workload'
    };
    const label = document.createElement('label');
    label.className = 'benchmark-card' +
      (selected.has(item.name) ? ' selected' : '') +
      (!config.enabled || !available || running ? ' disabled' : '');
    label.title = item.message || '';

    const input = document.createElement('input');
    input.type = 'checkbox';
    input.value = item.name;
    input.checked = selected.has(item.name);
    input.disabled = !config.enabled || !available || running;
    input.addEventListener('change', () => {
      if (input.checked) selected.add(item.name);
      else selected.delete(item.name);
      renderConfig();
    });

    const top = document.createElement('div');
    top.className = 'benchmark-top';
    const name = document.createElement('span');
    name.className = 'benchmark-name';
    name.textContent = item.name;
    const check = document.createElement('span');
    check.className = 'benchmark-check';
    check.textContent = '✓';
    top.append(name, check);

    const description = document.createElement('div');
    description.className = 'benchmark-desc';
    description.textContent = info.description;

    const chart = document.createElement('div');
    chart.className = 'mini-chart';
    for (let i = 0; i < 5; i++) chart.appendChild(document.createElement('i'));

    const foot = document.createElement('div');
    foot.className = 'benchmark-foot';
    const family = document.createElement('span');
    family.textContent = info.family;
    const asset = document.createElement('span');
    const preflightStatus = item.profile && item.profile.preflight && item.profile.preflight.status;
    asset.className = 'asset-state ' + (available
      ? (preflightStatus === 'warn' || preflightStatus === 'unsupported' ? 'warn' : 'ok')
      : 'bad');
    asset.textContent = available
      ? ((preflightStatus === 'warn' || preflightStatus === 'unsupported') ? '可运行 · 有警告' : '预检通过') +
        ' · ' + item.timeout_seconds + 's'
      : '未就绪';
    foot.append(family, asset);

    label.append(input, top, description, chart, foot);
    choices.appendChild(label);
  }

  const reasons = [];
  if (config.platform !== 'linux') reasons.push('第一版仅支持 Linux');
  if (!config.feature_enabled) reasons.push('stress.enabled 未启用');
  if (!config.web_enabled) reasons.push('stress.web_enabled 未启用');
  if (!config.loopback) reasons.push('Web 未绑定回环地址');
  if (!config.shared_report) reasons.push('report_path 未配置');
  if (selectable.length === 0) reasons.push('当前没有通过资产预检的项目');
  byId('policy').textContent = reasons.length
    ? 'Web 触发不可用：' + reasons.join('；')
    : '配置已就绪。选择一个或多个项目后启动，项目之间将串行执行。';
  byId('policy-state').textContent = reasons.length ? '需要配置' : '已就绪';
  byId('policy-state').className = 'policy-badge ' + (reasons.length ? 'blocked' : 'ready');
  byId('start').disabled = !config.enabled || running || selectable.length === 0;
  byId('cancel').hidden = !(running && report.cancellable);
  byId('progress-panel').hidden = !running;
  byId('progress-benchmark').textContent = activeBenchmark()
    ? '当前项目：' + activeBenchmark()
    : '正在准备运行环境…';
  renderProfilePreview();
  renderSummary();
}

function renderHistory() {
  const list = byId('history-list');
  const empty = byId('empty-history');
  const items = reportsForNavigation();
  list.innerHTML = '';
  byId('history-count').textContent = String(historyReports.length);
  empty.hidden = items.length > 0;

  for (const item of items) {
    const state = visual(item.status);
    const button = document.createElement('button');
    const isLatest = report && item.job_id === report.job_id;
    const active = viewedJobID ? item.job_id === viewedJobID : isLatest;
    button.type = 'button';
    button.className = 'history-item' + (active ? ' active' : '');
    button.addEventListener('click', () => {
      viewedJobID = isLatest ? '' : item.job_id;
      renderHistory();
      renderReport();
    });

    const top = document.createElement('div');
    top.className = 'history-item-top';
    const when = document.createElement('span');
    when.className = 'history-time';
    when.textContent = new Date(item.started_at).toLocaleString('zh-CN', {
      month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit'
    });
    const status = document.createElement('span');
    status.className = 'history-state ' + state.cls;
    status.textContent = item.status === 'running' ? '当前 · ' + state.label : state.label;
    top.append(when, status);

    const names = document.createElement('div');
    names.className = 'history-benchmarks';
    names.textContent = (item.benchmarks || []).map(value => value.name).join(' · ') || '--';
    const meta = document.createElement('div');
    meta.className = 'history-meta';
    meta.textContent = sourceLabel(item.initiator) + ' · ' + jobDuration(item);
    button.append(top, names, meta);
    list.appendChild(button);
  }
}

function createTrend(name, key, unit) {
  const series = benchmarkSeries(name, key);
  if (series.length < 2) return null;
  const width = 220;
  const height = 42;
  const maximum = Math.max(...series, 1) * 1.08;
  const points = series.map((value, index) => {
    const x = series.length === 1 ? width : index * width / (series.length - 1);
    const y = height - Math.max(0, value) / maximum * height;
    return x.toFixed(1) + ',' + y.toFixed(1);
  }).join(' ');

  const trend = document.createElement('div');
  trend.className = 'metric-trend';
  const head = document.createElement('div');
  head.className = 'metric-trend-head';
  const label = document.createElement('span');
  label.textContent = '最近 ' + series.length + ' 次同项结果';
  const scale = document.createElement('span');
  scale.textContent = '0 – ' + metricText(maximum, unit);
  head.append(label, scale);
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 ' + width + ' ' + height);
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', label.textContent);
  const baseline = document.createElementNS('http://www.w3.org/2000/svg', 'line');
  baseline.setAttribute('x1', '0');
  baseline.setAttribute('x2', String(width));
  baseline.setAttribute('y1', String(height));
  baseline.setAttribute('y2', String(height));
  baseline.setAttribute('stroke', '#cfd7e3');
  const line = document.createElementNS('http://www.w3.org/2000/svg', 'polyline');
  line.setAttribute('points', points);
  line.setAttribute('class', 'metric-trend-line');
  svg.append(baseline, line);
  trend.append(head, svg);
  return trend;
}

function detailRows(item) {
  const values = item.values || {};
  const rows = [];
  if (Number.isFinite(item.duration_ms)) {
    rows.push(['CATMonitor 总耗时', duration(item.duration_ms)]);
  }
  if (Number.isFinite(values.time_seconds)) {
    rows.push(['基准计算时间', metricText(values.time_seconds, 's')]);
  }
  if (item.name === 'hpl') {
    if (Number.isFinite(values.n)) rows.push(['问题规模 N', metricValue(values.n)]);
    if (Number.isFinite(values.nb)) rows.push(['块大小 NB', metricValue(values.nb)]);
    if (Number.isFinite(values.p) && Number.isFinite(values.q)) {
      rows.push(['进程网格 P × Q', metricValue(values.p) + ' × ' + metricValue(values.q)]);
    }
    if (Number.isFinite(values.process)) rows.push(['MPI 进程', metricValue(values.process)]);
  }
  if (item.source) {
    rows.push(['结果来源', item.source === 'result_file' ? '结果文件' : '标准输出']);
  }
  return rows;
}

function appendMetricContent(card, item, jobID) {
  const values = item.values || {};
  if ((item.name === 'hpl' || item.name === 'hpcg') && Number.isFinite(values.gflops)) {
    const primary = document.createElement('div');
    primary.className = 'primary-metric';
    const label = document.createElement('span');
    label.className = 'primary-metric-label';
    label.textContent = item.name === 'hpl' ? 'HPL 浮点性能' : 'HPCG 综合性能';
    const line = document.createElement('div');
    line.className = 'primary-metric-line';
    const value = document.createElement('strong');
    value.className = 'primary-metric-value';
    value.textContent = metricValue(values.gflops);
    const unit = document.createElement('span');
    unit.className = 'primary-metric-unit';
    unit.textContent = 'GFLOP/s';
    line.append(value, unit);
    primary.append(label, line);
    const trend = createTrend(item.name, 'gflops', 'GFLOP/s');
    if (trend) primary.appendChild(trend);
    card.appendChild(primary);
  }

  if (item.name === 'stream') {
    const performance = streamMetricOrder
      .filter(key => Number.isFinite(values[key]))
      .map(key => [key, values[key]]);
    if (performance.length) {
      const section = document.createElement('div');
      section.className = 'metric-section-label';
      section.textContent = '内存带宽';
      const metrics = document.createElement('div');
      metrics.className = 'metric-list';
      const scaleValues = performance.map(([, value]) => value);
      for (const [key, metricValueNumber] of performance) {
        const row = document.createElement('div');
        row.className = 'metric-row';
        const copy = document.createElement('div');
        copy.className = 'metric-copy';
        const name = document.createElement('span');
        name.className = 'metric-name';
        name.textContent = metricInfo[key].label;
        const bar = document.createElement('span');
        bar.className = 'metric-bar';
        const fill = document.createElement('span');
        fill.className = 'metric-fill';
        fill.style.width = metricWidth(metricValueNumber, scaleValues) + '%';
        bar.appendChild(fill);
        copy.append(name, bar);
        const rendered = document.createElement('span');
        rendered.className = 'metric-value';
        rendered.textContent = metricText(metricValueNumber, metricInfo[key].unit);
        row.append(copy, rendered);
        metrics.appendChild(row);
      }
      card.append(section, metrics);
    }
  }

  const details = detailRows(item);
  if (details.length) {
    const section = document.createElement('div');
    section.className = 'metric-section-label';
    section.textContent = '运行详情';
    const grid = document.createElement('div');
    grid.className = 'detail-grid';
    for (const [labelText, valueText] of details) {
      const detail = document.createElement('div');
      detail.className = 'detail-item';
      const label = document.createElement('span');
      label.className = 'detail-label';
      label.textContent = labelText;
      const value = document.createElement('span');
      value.className = 'detail-value';
      value.textContent = valueText;
      detail.append(label, value);
      grid.appendChild(detail);
    }
    card.append(section, grid);
  }

  if (!Object.keys(values).length) {
    const noMetrics = document.createElement('div');
    noMetrics.className = 'metric-name';
    noMetrics.textContent = item.status === 'time_limit_reached'
      ? '达到运行时限，按计划停止，未产生最终性能数值'
      : '尚无性能数值';
    card.appendChild(noMetrics);
  }

  if (item.profile) {
    const details = document.createElement('details');
    details.className = 'result-profile';
    const profileKey = (jobID || 'unknown-job') + ':' + item.name;
    details.open = expandedProfiles.has(profileKey);
    details.addEventListener('toggle', () => {
      if (details.open) expandedProfiles.add(profileKey);
      else expandedProfiles.delete(profileKey);
    });
    const summary = document.createElement('summary');
    summary.textContent = '查看当次执行 profile · ' + shortHash(item.profile.configuration_sha256);
    details.appendChild(summary);
    renderProfileBody(details, item.profile, 0);
    card.appendChild(details);
  }
}

function renderReport() {
  const shown = displayedReport();
  const state = visual(shown && shown.status);
  const status = byId('status');
  status.textContent = state.label;
  status.className = 'status-badge ' + state.cls;
  const resultCards = byId('result-cards');
  resultCards.innerHTML = '';
  const viewingHistory = Boolean(viewedJobID && shown && report && shown.job_id !== report.job_id);
  byId('report-kicker').textContent = viewingHistory ? 'HISTORY REPORT' : 'LATEST REPORT';
  byId('report-title').textContent = viewingHistory ? '历史可靠性压测详情' : '最近一次可靠性压测';
  byId('show-latest').hidden = !viewingHistory;

  if (!shown) {
    byId('meta').textContent = '尚无可靠性压测报告';
    byId('empty-results').hidden = false;
    renderSummary();
    return;
  }

  const external = shown.status === 'running' && !shown.cancellable
    ? ' · 此作业由其它进程启动，Web 仅监视进度'
    : '';
  byId('meta').textContent =
    '作业 ' + shown.job_id +
    ' · 触发来源：' + sourceLabel(shown.initiator) +
    ' · 开始时间：' + new Date(shown.started_at).toLocaleString('zh-CN') +
    ' · 配置：' + shortHash(shown.configuration_sha256) +
    external;
  byId('empty-results').hidden = true;

  for (const item of shown.benchmarks || []) {
    const itemState = visual(item.status);
    const card = document.createElement('article');
    card.className = 'result-card';
    const head = document.createElement('div');
    head.className = 'result-card-head';
    const name = document.createElement('span');
    name.className = 'result-card-name';
    name.textContent = item.name;
    const resultState = document.createElement('span');
    resultState.className = 'result-state ' + itemState.cls;
    resultState.textContent = itemState.label;
    head.append(name, resultState);

    card.appendChild(head);
    appendMetricContent(card, item, shown.job_id);
    if (item.message) {
      const message = document.createElement('div');
      message.className = 'result-message';
      message.textContent = item.message;
      card.appendChild(message);
    }
    resultCards.appendChild(card);
  }
  renderSummary();
}

async function readJSON(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || ('HTTP ' + response.status));
  return body;
}

async function refresh() {
  try {
    config = await readJSON(await fetch('/api/stress/config', {cache: 'no-store'}));
    const latest = await fetch('/api/stress/latest', {cache: 'no-store'});
    report = latest.ok ? await latest.json() : null;
    const historyResponse = await fetch('/api/stress/history?limit=100', {cache: 'no-store'});
    historyReports = historyResponse.ok ? await historyResponse.json() : [];
    if (viewedJobID && !reportsForNavigation().some(item => item.job_id === viewedJobID)) {
      viewedJobID = '';
    }
    setError('');
    renderConfig();
    renderHistory();
    renderReport();
  } catch (error) {
    byId('policy').textContent = '配置接口读取失败：' + error.message;
    byId('policy-state').textContent = '加载失败';
    byId('policy-state').className = 'policy-badge blocked';
    setError(error.message);
  }
}

async function start() {
  setError('');
  const benchmarks = Array.from(selected || []);
  if (!benchmarks.length) {
    setError('请至少选择一个已就绪的可靠性压测项目。');
    return;
  }
  const raw = byId('timeout').value.trim();
  const timeoutSeconds = raw ? Number(raw) : 0;
  if (!Number.isInteger(timeoutSeconds) || timeoutSeconds < 0) {
    setError('单次超时必须是正整数秒。');
    return;
  }
  if (timeoutSeconds) {
    const maximum = Math.min(...config.benchmarks
      .filter(item => benchmarks.includes(item.name))
      .map(item => item.timeout_seconds));
    if (timeoutSeconds > maximum) {
      setError('单次超时只能缩短，不能超过所选项目的最小上限 ' + maximum + ' 秒。');
      return;
    }
  }
  const profileLines = (config.benchmarks || [])
    .filter(item => benchmarks.includes(item.name))
    .map(item => {
      const profile = item.profile || {};
      const resources = profile.resources || {};
      const workers = resources.total_workers > 0 ? resources.total_workers + ' 工作单元' : '工作单元自动';
      return item.name.toUpperCase() + '：' + workers +
        '，时限 ' + (timeoutSeconds || profile.timeout_seconds || item.timeout_seconds) + 's' +
        '，配置 ' + (timeoutSeconds ? '提交后生成新哈希' : shortHash(profile.configuration_sha256)) +
        (profile.preflight && profile.preflight.status === 'warn' ? ' [预检警告]' : '');
    });
  if (!window.confirm(
    '将启动高负载可靠性压测：\n\n' + profileLines.join('\n') +
    '\n\n参数只读，确认按以上 profile 执行吗？'
  )) return;
  try {
    report = await readJSON(await fetch('/api/stress/runs', {
      method: 'POST',
      headers: {'Content-Type': 'application/json', 'X-CATMonitor-Action': 'stress'},
      body: JSON.stringify({benchmarks, timeout_seconds: timeoutSeconds})
    }));
    viewedJobID = '';
    renderConfig();
    renderHistory();
    renderReport();
  } catch (error) {
    setError(error.message);
  }
}

async function cancel() {
  if (!report || !report.job_id) return;
  if (!window.confirm('确认停止当前可靠性压测？')) return;
  try {
    await readJSON(await fetch('/api/stress/runs/' + encodeURIComponent(report.job_id) + '/cancel', {
      method: 'POST',
      headers: {'Content-Type': 'application/json', 'X-CATMonitor-Action': 'stress'},
      body: '{}'
    }));
    setTimeout(refresh, 150);
  } catch (error) {
    setError(error.message);
  }
}

byId('start').addEventListener('click', start);
byId('cancel').addEventListener('click', cancel);
byId('timeout').addEventListener('input', renderProfilePreview);
byId('show-latest').addEventListener('click', () => {
  viewedJobID = '';
  renderHistory();
  renderReport();
});
window.addEventListener('load', () => {
  if (!byId('stress-style').sheet) {
    setError('样式资源未加载，请检查 /stress/static/stress.css。');
  }
});
refresh();
setInterval(refresh, 2000);

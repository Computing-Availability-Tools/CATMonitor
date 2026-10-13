'use strict';

window.clearTimeout(window.__stressBootTimer);

let config = null;
let report = null;
let selected = null;
let selectedNPUDevices = null;
let selectedNPUCases = null;
let npuMode = 'operators';
let selectedNPUModel = '';
let npuModelAction = 'compare';
let refreshInFlight = false;
let configRefreshedAt = 0;
let historyReports = [];
let viewedJobID = '';
const expandedProfiles = new Set();

const byId = id => document.getElementById(id);
const benchmarkInfo = {
  stream: {description: '内存带宽'},
  hpl: {description: '浮点计算 · MPI'},
  hpcg: {description: '综合计算 · 内存 · MPI'},
  npu_burn: {description: 'NPU 计算 · SDC 检测'}
};
const benchmarkDisplayOrder = ['hpcg', 'hpl', 'stream', 'npu_burn'];
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
  process: {label: 'MPI 进程', unit: ''},
  devices: {label: 'NPU 设备', unit: 'cards'},
  cases: {label: '用例结果', unit: 'cases'},
  passed: {label: '通过用例', unit: 'cases'},
  failed: {label: '失败用例', unit: 'cases'},
  errors: {label: '检测错误', unit: ''},
  case_time_seconds: {label: '累计用例时间', unit: 's'}
};
const streamMetricOrder = ['copy_mb_s', 'scale_mb_s', 'add_mb_s', 'triad_mb_s'];
const preflightLabels = {
  pass: '预检通过',
  warn: '预检有警告',
  fail: '预检失败',
  unsupported: '不支持'
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
function npuJobDuration(job, item) {
  const start = job && new Date(job.started_at).getTime();
  const end = job && (job.finished_at ? new Date(job.finished_at).getTime()
    : (job.status === 'running' ? Date.now() : NaN));
  const ms = Number.isFinite(start) && Number.isFinite(end) && end >= start ? end - start : item.duration_ms;
  if (!Number.isFinite(ms) || ms < 0) return '--';
  const units = ms >= 3600000 ? [3600000, '小时'] : (ms >= 60000 ? [60000, '分钟'] : [1000, '秒']);
  return new Intl.NumberFormat('zh-CN', {maximumFractionDigits: 2}).format(ms / units[0]) + ' ' + units[1];
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

// New reports use the canonical NPU Burn protocol keys. Keep the aliases here,
// at the read-only presentation boundary, so reports written by older versions
// remain viewable without making the backend emit two schemas.
function npuBurnValues(values) {
  values = values || {};
  return {
    ...values,
    devices: values.devices ?? values.device_count,
    cases: values.cases ?? values.case_count,
    passed: values.passed ?? values.passed_case_count,
    failed: values.failed ?? values.failed_case_count,
    errors: values.errors ?? values.error_count
  };
}

function resultValues(item) {
  return item.name === 'npu_burn' ? npuBurnValues(item.values) : (item.values || {});
}
function profileParameters(profile) {
  const values = {};
  for (const item of (profile && profile.parameters) || []) values[item.key] = item.value;
  return values;
}
function npuDeploymentSummary(profile) {
  const values = profileParameters(profile);
  if (!values.backend) return '';
  const parts = ['执行：' + values.backend];
  if (values.container) parts.push('容器：' + values.container);
  if (values.image) parts.push('镜像：' + values.image);
  if (values.cann) parts.push('CANN：' + values.cann);
  if (values.torch_npu) parts.push('torch_npu：' + values.torch_npu);
  if (values.soc) parts.push('SoC：' + values.soc);
  if (values.device_namespace) parts.push('设备命名空间：' + values.device_namespace);
  if (values.available_devices) parts.push('可用 logical ID：' + values.available_devices);
  if (values.topology_source) parts.push('拓扑来源：' + values.topology_source);
  if (values.pci_topology_devices) parts.push('PCI logical ID：' + values.pci_topology_devices);
  return parts.join(' · ');
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
    if (item.key === 'case_catalog') {
      const details = document.createElement('details');
      const summary = document.createElement('summary');
      summary.textContent = '算子准入原始数据（不是实机结果）';
      const raw = document.createElement('pre');
      raw.textContent = item.value;
      details.append(summary, raw);
      parameters.appendChild(details);
      continue;
    }
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
  appendResource(strip, '配置最大作业时限', (profileParameters(profile).maximum_job_timeout || profile.timeout_seconds) + ' s');
  appendResource(strip, '本次有效时限', effectiveTimeout + ' s');
  appendResource(strip, '总工作单元', resources.total_workers > 0 ? resources.total_workers : '运行时确定');
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
    check.textContent = asset.name + ' · ' +
      (asset.status === 'pass' ? '可用' : profileValue(asset.message, '失败'));
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
      if (item.name === 'npu_burn') {
        const p = profileParameters(item.profile);
        const strip = document.createElement('div');
        strip.className = 'resource-strip';
        appendResource(strip, '芯片代际', p.chip_generation || '未确认');
        appendResource(strip, 'SDC 检测', p.sdc_detection === 'enabled' ? '启用' : profileValue(p.sdc_detection));
        appendResource(strip, '配置最大作业时限', (p.maximum_job_timeout || item.profile.timeout_seconds) + ' s');
        appendResource(strip, '本次有效时限', (timeoutOverride > 0 && timeoutOverride <= item.profile.timeout_seconds
          ? timeoutOverride : item.profile.timeout_seconds) + ' s');
        card.appendChild(strip);
        const details = disclosure('preview:npu', '部署参数与预检详情');
        renderProfileBody(details, item.profile, timeoutOverride);
        card.appendChild(details);
      } else {
        renderProfileBody(card, item.profile, timeoutOverride);
      }
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

  const displayBenchmarks = (config.benchmarks || []).slice().sort((a, b) =>
    benchmarkDisplayOrder.indexOf(a.name) - benchmarkDisplayOrder.indexOf(b.name));
  for (const item of displayBenchmarks) {
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

    const foot = document.createElement('div');
    foot.className = 'benchmark-foot';
    const asset = document.createElement('span');
    const preflightStatus = item.profile && item.profile.preflight && item.profile.preflight.status;
    asset.className = 'asset-state ' + (available
      ? (preflightStatus === 'warn' || preflightStatus === 'unsupported' ? 'warn' : 'ok')
      : 'bad');
    asset.textContent = available
      ? ((preflightStatus === 'warn' || preflightStatus === 'unsupported') ? '可运行 · 有警告' : '预检通过') +
        ' · ' + item.timeout_seconds + 's'
      : (item.enabled ? '未就绪' : '未启用');
    foot.append(asset);

    label.append(input, top, description, foot);
    if (item.enabled && !available && item.message) {
      const reason = document.createElement('div');
      reason.className = 'benchmark-reason';
      reason.textContent = item.message;
      label.appendChild(reason);
    }
    choices.appendChild(label);
  }

  const reasons = [];
  if (config.platform !== 'linux') reasons.push('第一版仅支持 Linux');
  if (!config.feature_enabled) reasons.push('stress.enabled 未启用');
  if (!config.web_enabled) reasons.push('stress.web_enabled 未启用');
  if (!config.shared_report) reasons.push('report_path 未配置');
  if (selectable.length === 0) reasons.push('当前没有通过资产预检的项目');
  byId('policy').textContent = reasons.length
    ? 'Web 触发不可用：' + reasons.join('；')
    : '多个项目按顺序执行。';
  byId('policy-state').textContent = reasons.length ? '需要配置' : '已就绪';
  byId('policy').hidden = !reasons.length;
  byId('policy-state').className = 'policy-badge ' + (reasons.length ? 'blocked' : 'ready');
  byId('start').disabled = !config.enabled || running || selectable.length === 0;
  byId('start-label').textContent = '开始压测';
  byId('cancel').hidden = !(running && report.cancellable);
  byId('progress-panel').hidden = !running;
  byId('progress-benchmark').textContent = activeBenchmark()
    ? '当前项目：' + activeBenchmark()
    : '正在准备运行环境…';
  renderProfilePreview();
  renderNPUSelection();
  renderSummary();
}

function logicalIDs(value) {
  if (!/^\d+(,\d+)*$/.test(value || '')) return [];
  return value.split(',').map(Number);
}

function isNPUModel(candidate) {
  return ['gpt3', 'llama2'].includes(candidate.name) ||
    (candidate.reason || '').includes('separately prepared SDC reference');
}

function unavailableNPUReason(candidate) {
  if (isNPUModel(candidate)) return '模型测试需另行准备 SDC 参考数据，不属于算子选择。';
  if ((candidate.reason || '').includes('no upstream A2 QuantMatmul')) {
    return '上游未提供 A2 的量化矩阵乘策略，不能套用 A3/A5 策略。';
  }
  return candidate.reason || '当前代际、dtype 或镜像配置未通过准入检查。';
}

function renderNPUSelection() {
  const item = (config.benchmarks || []).find(b => b.name === 'npu_burn');
  const field = byId('npu-selection');
  field.hidden = !item || !selected || !selected.has('npu_burn');
  if (field.hidden) return;
  const p = profileParameters(item.profile || {});
  const models = (p.allowed_models || '').split(',').filter(name => ['gpt3', 'llama2'].includes(name));
  const memoryPolicy = byId('npu-model-memory');
  if (memoryPolicy) memoryPolicy.textContent = '完整参数 · ' + (p.model_memory_mode === 'checkpointed' ? '激活重计算' : (p.model_memory_mode === 'standard' ? '标准执行' : '显存策略未记录'));
  if (!models.includes(selectedNPUModel)) selectedNPUModel = models[0] || '';
  const modelChoices = byId('npu-model-choices');
  modelChoices.replaceChildren();
  for (const name of models) {
    const choice = document.createElement('button');
    choice.type = 'button';
    choice.value = name;
    choice.className = 'model-choice-button';
    choice.textContent = name === 'gpt3' ? 'GPT3' : 'Llama2';
    choice.setAttribute('aria-pressed', String(selectedNPUModel === name));
    choice.onclick = () => { selectedNPUModel = name; renderNPUSelection(); };
    modelChoices.appendChild(choice);
  }
  if (!models.length) npuMode = 'operators';
  const operatorMode = byId('npu-mode-operators');
  const modelMode = byId('npu-mode-model');
  modelMode.disabled = !models.length;
  operatorMode.setAttribute('aria-pressed', String(npuMode === 'operators'));
  modelMode.setAttribute('aria-pressed', String(npuMode === 'model'));
  operatorMode.onclick = () => { npuMode = 'operators'; renderNPUSelection(); };
  modelMode.onclick = () => { if (models.length) { npuMode = 'model'; renderNPUSelection(); } };
  byId('npu-model-availability').hidden = !!models.length;
  byId('npu-model-availability').textContent = '模型 SDC 尚未在当前部署启用；算子压测不受影响。';
  const mayPrepareReference = p.allow_reference_generation === 'true';
  if (!mayPrepareReference) npuModelAction = 'compare';
  byId('npu-model-prepare').disabled = !mayPrepareReference;
  byId('npu-reference-permission').hidden = mayPrepareReference;
  byId('npu-model-prepare').onclick = () => {
    if (mayPrepareReference) { npuModelAction = 'create_reference'; renderNPUSelection(); }
  };
  byId('npu-model-compare').onclick = () => { npuModelAction = 'compare'; renderNPUSelection(); };
  const preparing = npuModelAction === 'create_reference';
  byId('npu-model-preparing').hidden = !preparing;
  byId('npu-reference-setup').hidden = preparing;
  byId('npu-reference-label').textContent = preparing ? '新参考 ID（不能覆盖已有参考）' : '对照参考 ID';
  byId('npu-reference-id').placeholder = preparing ? '例如 gpt3-a2-baseline-01' : '填写已生成的参考 ID';
  if (npuMode === 'model' && selected.size === 1) {
    byId('start-label').textContent = preparing ? '生成参考' : '开始 SDC 检测';
  } else {
    byId('start-label').textContent = '开始压测';
  }
  byId('npu-model-selection').hidden = npuMode !== 'model';
  byId('npu-operator-heading').hidden = npuMode === 'model';
  byId('npu-case-choices').hidden = npuMode === 'model';
  byId('npu-case-count').hidden = npuMode === 'model';
  byId('npu-case-hint').hidden = npuMode === 'model';
  const available = logicalIDs(p.available_devices);
  const defaults = p.devices === 'all' ? available : logicalIDs(p.devices);
  const allowed = p.allowed_devices === 'all' ? available : logicalIDs(p.allowed_devices || p.devices);
  if (selectedNPUDevices === null) selectedNPUDevices = new Set(defaults);
  selectedNPUDevices = new Set(Array.from(selectedNPUDevices).filter(id => available.includes(id) && allowed.includes(id)));
  field.disabled = !!report && report.status === 'running';
  const list = byId('npu-device-choices');
  list.replaceChildren();
  for (const id of available) {
    const label = document.createElement('label');
    label.className = 'npu-choice';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = selectedNPUDevices.has(id);
    input.disabled = !allowed.includes(id);
    input.addEventListener('change', () => {
      if (input.checked) selectedNPUDevices.add(id); else selectedNPUDevices.delete(id);
      renderNPUSelection();
    });
    label.append(input, document.createTextNode(' logical ' + id + (input.disabled ? '（未授权）' : '') + ' '));
    list.append(label);
  }
  byId('npu-select-all').onclick = () => {
    selectedNPUDevices = new Set(available.filter(id => allowed.includes(id)));
    renderNPUSelection();
  };
  let catalog = [];
  try { catalog = JSON.parse(p.case_catalog || '[]'); } catch (_) { /* unavailable image contract */ }
  const defaultsCases = (p.selected_cases || '').split(',').filter(Boolean);
  const selectableCases = catalog.filter(item => item.selectable && !isNPUModel(item));
  const availableCases = selectableCases.map(item => item.name);
  if (selectedNPUCases === null) selectedNPUCases = new Set(defaultsCases);
  selectedNPUCases = new Set(Array.from(selectedNPUCases).filter(name => availableCases.includes(name)));
  byId('npu-case-count').textContent = '已选 ' + selectedNPUCases.size + ' 项';
  byId('npu-case-hint').textContent = catalog.length
    ? '预检通过不等于实机通过，实际运行结果以作业报告为准。'
    : '当前镜像未提供算子目录；仅使用部署默认用例。';
  const cases = byId('npu-case-choices');
  cases.replaceChildren();
  for (const candidate of selectableCases) {
    const label = document.createElement('label');
    label.className = 'npu-choice';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = selectedNPUCases.has(candidate.name);
    input.addEventListener('change', () => {
      if (input.checked) selectedNPUCases.add(candidate.name); else selectedNPUCases.delete(candidate.name);
      renderNPUSelection();
    });
    const name = document.createElement('span');
    name.textContent = candidate.name;
    label.append(input, name);
    cases.append(label);
  }
  const unavailable = catalog.filter(item => !item.selectable || isNPUModel(item));
  byId('npu-unavailable').hidden = !unavailable.length;
  const unavailableList = byId('npu-unavailable-items');
  unavailableList.replaceChildren();
  for (const candidate of unavailable) {
    const row = document.createElement('div');
    row.className = 'unavailable-item';
    const name = document.createElement('b');
    name.textContent = candidate.name;
    row.append(name, document.createTextNode(' · ' + unavailableNPUReason(candidate)));
    unavailableList.appendChild(row);
  }
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
  const values = resultValues(item);
  const rows = [];
  if (Number.isFinite(item.duration_ms)) {
    rows.push([item.name === 'npu_burn' ? '执行阶段耗时' : 'CATMonitor 总耗时', duration(item.duration_ms)]);
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
  if (item.name === 'npu_burn') {
    if (item.npu_selection) {
      const s = item.npu_selection;
      rows.push(['部署默认逻辑 ID', (s.deployment_default || []).join(',')]);
      rows.push(['本次请求逻辑 ID', s.requested ? s.requested.join(',') : '使用部署默认']);
      rows.push(['本次固定逻辑 ID', (s.selected || []).join(',')]);
      rows.push(['CSV 验证逻辑 ID', s.actual ? s.actual.join(',') : '尚未验证']);
      if (s.selected_cases) {
        rows.push(['部署默认算子', (s.default_cases || []).join(',')]);
        rows.push(['本次请求算子', s.requested_cases ? s.requested_cases.join(',') : '使用部署默认']);
        rows.push(['本次固定算子', s.selected_cases.join(',')]);
        rows.push(['CSV 验证算子', s.actual_cases ? s.actual_cases.join(',') : '尚未验证']);
      }
    }
    if (Number.isFinite(values.case_time_seconds)) {
      rows.push(['累计用例时间', metricText(values.case_time_seconds, 's')]);
    }
  }
  if (item.source) {
    const source = item.source === 'result_file'
      ? '结果文件'
      : (item.source === 'result_csv' ? 'NPU Burn CSV' : '标准输出');
    rows.push(['结果来源', source]);
  }
  return rows;
}

function disclosure(key, title) {
  const details = document.createElement('details');
  details.className = 'result-profile';
  details.open = expandedProfiles.has(key);
  details.addEventListener('toggle', () => {
    if (details.open) expandedProfiles.add(key);
    else expandedProfiles.delete(key);
  });
  const summary = document.createElement('summary');
  summary.textContent = title;
  details.appendChild(summary);
  return details;
}

// The plugin emits these groups only after validating this job's CSV. Never
// invent per-device counts by dividing the report's total number of rows.
function npuCSVGroups(item) {
  const prefix = 'CATMONITOR_NPU_BURN_CASE_GROUPS=';
  const lines = (item.output || '').split('\n').filter(line => line.startsWith(prefix));
  if (!item.npu_csv && !lines.length) return {groups: [], error: ''};
  try {
    const groups = item.npu_csv ? item.npu_csv.groups : JSON.parse(lines[lines.length - 1].slice(prefix.length));
    const keys = new Set();
    if (!Array.isArray(groups) || !groups.length) throw new Error();
    const totals = {cases: 0, passed: 0, failed: 0, errors: 0};
    for (const g of groups) {
      const key = g.device + ':' + g.operator;
      if (!Number.isSafeInteger(g.device) || g.device < 0 || typeof g.operator !== 'string' ||
          !g.operator.trim() || keys.has(key) || !Number.isFinite(g.case_time_seconds) ||
          g.case_time_seconds < 0) throw new Error();
      keys.add(key);
      for (const field of Object.keys(totals)) {
        if (!Number.isSafeInteger(g[field]) || g[field] < 0) throw new Error();
        totals[field] += g[field];
      }
      if (g.cases < 1 || g.passed + g.failed !== g.cases) throw new Error();
    }
    const values = resultValues(item);
    if (!Object.keys(totals).every(key => totals[key] === values[key])) throw new Error();
    if (Number.isFinite(values.devices) && new Set(groups.map(g => g.device)).size !== values.devices) throw new Error();
    const seconds = groups.reduce((sum, g) => sum + g.case_time_seconds, 0);
    if (Number.isFinite(values.case_time_seconds) && Math.abs(seconds - values.case_time_seconds) > 0.000001) throw new Error();
    return {groups: groups.slice().sort((a, b) => a.device - b.device || a.operator.localeCompare(b.operator)), error: ''};
  } catch (_) {
    return {groups: [], error: 'CSV 分组数据无效或与报告总数不一致，不能展示为完整结果。'};
  }
}

function sameSelection(a, b) {
  return a.length === b.length && new Set(a).size === a.length && a.every(value => b.includes(value));
}

function npuResultIdentity(item, groups) {
  const selection = item.npu_selection || {};
  const p = profileParameters(item.profile);
  const expectedDevices = selection.selected || (p.devices === 'all' ? logicalIDs(p.available_devices) : logicalIDs(p.devices));
  const expectedCases = selection.selected_cases || (p.selected_cases || '').split(',').filter(Boolean);
  const actualDevices = selection.actual || Array.from(new Set(groups.map(g => g.device)));
  const actualCases = selection.actual_cases || Array.from(new Set(groups.map(g => g.operator)));
  const differences = [];
  const devicesText = ids => ids.length ? ids.map(id => 'logical ' + id).join('、') : '未记录';
  const casesText = names => names.join('、') || '未记录';
  if (actualDevices.length && expectedDevices.length && !sameSelection(expectedDevices, actualDevices)) {
    differences.push('设备不一致：本次固定 ' + devicesText(expectedDevices) + '；CSV 实际 ' + devicesText(actualDevices));
  }
  if (actualCases.length && expectedCases.length && !sameSelection(expectedCases, actualCases)) {
    differences.push('算子不一致：本次固定 ' + casesText(expectedCases) + '；CSV 实际 ' + casesText(actualCases));
  }
  if (selection.requested && !sameSelection(selection.requested, expectedDevices)) {
    differences.push('请求设备与本次固定设备不一致：请求 ' + devicesText(selection.requested) + '；固定 ' + devicesText(expectedDevices));
  }
  if (selection.requested_cases && !sameSelection(selection.requested_cases, expectedCases)) {
    differences.push('请求算子与本次固定算子不一致：请求 ' + casesText(selection.requested_cases) + '；固定 ' + casesText(expectedCases));
  }
  // Even internally consistent grouped totals cannot override differing CSV
  // identity metadata. Show that disagreement outside collapsed trace details.
  if (groups.length && actualDevices.length && !sameSelection(actualDevices, Array.from(new Set(groups.map(g => g.device))))) {
    differences.push('CSV 分组设备与报告核验设备不一致。');
  }
  if (groups.length && actualCases.length && !sameSelection(actualCases, Array.from(new Set(groups.map(g => g.operator))))) {
    differences.push('CSV 分组算子与报告核验算子不一致。');
  }
  if (groups.length) {
    for (const id of expectedDevices) {
      for (const name of expectedCases) {
        if (!groups.some(g => g.device === id && g.operator === name)) {
          differences.push('CSV 缺少 logical ' + id + ' / ' + name + ' 的结果。');
        }
      }
    }
  }
  return {devices: devicesText(actualDevices), cases: casesText(actualCases), differences};
}

function appendDetailGrid(container, rows) {
  const grid = document.createElement('div');
  grid.className = 'detail-grid';
  for (const [labelText, valueText] of rows) {
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
  container.appendChild(grid);
}

function appendNPUResult(card, item, jobID, job) {
  const values = resultValues(item);
  const breakdown = npuCSVGroups(item);
  const identity = npuResultIdentity(item, breakdown.groups);
  const copy = document.createElement('div');
  copy.className = 'npu-result-identity';
  for (const text of ['设备：' + identity.devices, '算子：' + identity.cases]) {
    const line = document.createElement('div');
    line.textContent = text;
    copy.appendChild(line);
  }
  card.appendChild(copy);
  const differences = identity.differences.concat(breakdown.error ? [breakdown.error] : []);
  if (differences.length) {
    const warning = document.createElement('div');
    warning.className = 'npu-result-warning';
    warning.setAttribute('role', 'alert');
    warning.textContent = '结果核验差异：' + differences.join('；');
    card.appendChild(warning);
  }
  const total = document.createElement('div');
  total.className = 'npu-case-total';
  if (Number.isFinite(values.cases) && Number.isFinite(values.passed) && Number.isFinite(values.errors)) {
    total.textContent = metricValue(values.passed) + ' / ' + metricValue(values.cases) + ' 用例通过 · 检测错误 ' + metricValue(values.errors);
    if (values.failed > 0 || values.errors > 0 || differences.length) total.className += ' bad';
  } else {
    total.className = 'selection-hint';
    total.textContent = '尚无完整 CSV 用例结果，不能认定通过。';
  }
  card.appendChild(total);
  const elapsed = document.createElement('div');
  elapsed.className = 'duration-row';
  elapsed.textContent = '作业耗时：' + npuJobDuration(job, item);
  card.appendChild(elapsed);

  const details = disclosure(jobID + ':npu:groups', '分设备、分算子结果');
  const hint = document.createElement('p');
  hint.className = 'selection-hint';
  hint.textContent = '用例数是 CSV 结果记录数，不是算子数、设备数或迭代次数。';
  details.appendChild(hint);
  if (breakdown.groups.length) {
    const scroll = document.createElement('div');
    scroll.className = 'npu-table-scroll';
    const table = document.createElement('table');
    table.className = 'npu-group-table';
    const header = document.createElement('tr');
    for (const title of ['逻辑设备', '算子', '用例通过数', '检测错误']) {
      const th = document.createElement('th');
      th.textContent = title;
      th.setAttribute('scope', 'col');
      header.appendChild(th);
    }
    const thead = document.createElement('thead');
    thead.appendChild(header);
    const tbody = document.createElement('tbody');
    for (const g of breakdown.groups) {
      const row = document.createElement('tr');
      if (g.failed || g.errors) row.className = 'bad';
      for (const text of [String(g.device), g.operator, g.passed + ' / ' + g.cases, String(g.errors)]) {
        const td = document.createElement('td');
        td.textContent = text;
        row.appendChild(td);
      }
      tbody.appendChild(row);
    }
    table.append(thead, tbody);
    scroll.appendChild(table);
    details.appendChild(scroll);
  } else {
    const empty = document.createElement('p');
    empty.className = 'selection-hint';
    empty.textContent = breakdown.error || (Number.isFinite(values.cases)
      ? '该报告未保存 CSV 分组明细；保留原有总数，不推算各设备、算子的用例数。'
      : '当前作业尚无已核验的 CSV 分组明细。');
    details.appendChild(empty);
  }
  card.appendChild(details);

  const trace = disclosure(jobID + ':npu:trace', '执行配置与结果文件');
  appendDetailGrid(trace, detailRows(item));
  const timing = document.createElement('p');
  timing.className = 'selection-hint';
  timing.textContent = '累计用例时间是 CSV 各用例时间之和；设备可能并行执行。作业耗时按作业起止时间计算，包含启动、执行及收尾，不含提交前准入检查；两者不同。';
  trace.appendChild(timing);
  if (item.profile) renderProfileBody(trace, item.profile, 0);
  const csv = (item.output || '').match(/^CATMONITOR_NPU_BURN_RESULT_CSV=(.+)$/m);
  const csvPath = (item.npu_csv && item.npu_csv.path) || (csv && csv[1].trim());
  if (csvPath) appendDetailGrid(trace, [['本轮 CSV 文件', csvPath]]);
  if (item.output) {
    const output = document.createElement('details');
    const title = document.createElement('summary');
    title.textContent = '执行输出';
    const raw = document.createElement('pre');
    raw.textContent = item.output;
    output.append(title, raw);
    trace.appendChild(output);
  }
  card.appendChild(trace);
}

function isHBMOOM(item) {
  return item && item.status === 'unhealthy' && item.failure && item.failure.code === 'npu_hbm_oom' && item.failure.resource === 'npu_hbm';
}

function appendMetricContent(card, item, jobID, job) {
  if (isHBMOOM(item)) {
    const message = document.createElement('div');
    message.className = 'resource-warning';
    message.textContent = '显存不足，本次压测未完成；不等于设备硬件故障。';
    card.appendChild(message);
    const request = (item.npu_selection || {}).model || {};
    if (request.name) appendDetailGrid(card, [['模型',request.name],['逻辑设备',((item.npu_selection || {}).selected || []).join(',')]]);
    const details = document.createElement('details');
    const summary = document.createElement('summary');
    summary.textContent = '执行详情';
    details.appendChild(summary);
    if (item.profile) renderProfileBody(details, item.profile, 0);
    const raw = document.createElement('pre');
    raw.textContent = item.output || item.message || '';
    details.appendChild(raw);
    card.appendChild(details);
    return;
  }
  if (item.npu_model || (item.npu_selection && item.npu_selection.model)) {
    const request = (item.npu_selection || {}).model || {};
    const result = item.npu_model;
    const title = document.createElement('div');
    title.className = 'npu-case-total';
    title.textContent = result && item.status === 'healthy' ? (result.outcome === 'reference_generated' ? '参考生成成功（不是 SDC 检测通过）' : '模型 SDC 对照通过') : '模型任务未通过或尚未完成：' + item.status;
    card.appendChild(title);
    appendDetailGrid(card, [['模型', result ? result.model : request.name], ['操作', result ? result.action : request.action], ['参考 ID', result ? result.reference_id : request.reference_id], ['逻辑设备', result ? result.devices.join(',') : ((item.npu_selection || {}).selected || []).join(',')]]);
    if (result) appendDetailGrid(card, [['比较步数',result.checked_steps], ['保存步数',result.saved_steps], ['检测错误',result.errors], ['显存策略',result.memory_mode === 'checkpointed' ? '完整参数 · 激活重计算' : (result.memory_mode === 'standard' ? '标准' : '未记录')], ['本轮 CSV',result.csv]]);
    return;
  }
  if (item.name === 'npu_burn') {
    appendNPUResult(card, item, jobID, job);
    return;
  }
  const values = resultValues(item);
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
    card.appendChild(section);
    appendDetailGrid(card, details);
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
    const profileKey = (jobID || 'unknown-job') + ':' + item.name;
    const details = disclosure(profileKey, '查看当次执行 profile · ' + shortHash(item.profile.configuration_sha256));
    renderProfileBody(details, item.profile, 0);
    card.appendChild(details);
  }
}

function renderReport() {
  const shown = displayedReport();
  const state = visual(shown && shown.status);
  const incomplete = ((shown && shown.benchmarks) || []).filter(item => item.status !== 'healthy' && item.status !== 'time_limit_reached');
  if (shown && shown.status === 'unhealthy' && incomplete.length && incomplete.every(isHBMOOM)) {
    state.label = '显存不足 · 未完成'; state.cls = 'warn';
  }
  const status = byId('status');
  status.textContent = state.label;
  status.className = 'status-badge ' + state.cls;
  const resultCards = byId('result-cards');
  resultCards.innerHTML = '';
  const viewingHistory = Boolean(viewedJobID && shown && report && shown.job_id !== report.job_id);
  byId('report-kicker').textContent = viewingHistory ? 'HISTORY REPORT' : 'LATEST REPORT';
  byId('report-title').textContent = viewingHistory ? '历史结果' : '本次结果';
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
    sourceLabel(shown.initiator) +
    ' · ' + new Date(shown.started_at).toLocaleString('zh-CN') +
    external;
  byId('meta').title = '作业 ' + shown.job_id + ' · 配置 ' + (shown.configuration_sha256 || '未记录');
  byId('empty-results').hidden = true;

  for (const item of shown.benchmarks || []) {
    const itemState = isHBMOOM(item) ? {cls:'warn',label:'显存不足 · 未完成'} : visual(item.status);
    if (item.status === 'healthy') itemState.label = item.npu_model && item.npu_model.outcome === 'reference_generated' ? '参考已生成' : '压测通过';
    const card = document.createElement('article');
    card.className = 'result-card' + (item.name === 'npu_burn' ? ' npu-result' : '');
    const head = document.createElement('div');
    head.className = 'result-card-head';
    const name = document.createElement('span');
    name.className = 'result-card-name';
    name.textContent = item.name === 'npu_burn' ? 'NPU Burn' : item.name;
    const resultState = document.createElement('span');
    resultState.className = 'result-state ' + itemState.cls;
    resultState.textContent = itemState.label;
    head.append(name, resultState);

    card.appendChild(head);
    appendMetricContent(card, item, shown.job_id, shown);
    if (item.message && !isHBMOOM(item) && (item.name !== 'npu_burn' || item.status !== 'healthy')) {
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
  let body;
  try {
    body = await response.json();
  } catch (_) {
    throw new Error('接口未返回有效 JSON，请检查访问地址或反向代理配置。');
  }
  if (!response.ok) throw new Error(body.error || ('HTTP ' + response.status));
  return body;
}

function refreshFailure(error) {
  // A transport failure is not a failed workload/preflight verdict. Keep the
  // last successfully loaded selections/report, but do not present them as
  // fresh or allow a new operator request until connectivity recovers.
  const networkError = error && error.name === 'TypeError' &&
    /failed to fetch|load failed|networkerror|network request failed/i.test(error.message || '');
  return networkError
    ? {label: '连接中断', message: '无法连接 Web 服务，请检查地址和网络；通过 SSH 访问时，请确认隧道仍在运行。'}
    : {label: '读取失败', message: '压测数据读取失败：' + error.message};
}

async function refresh() {
  // NPU describe imports the runtime. Do not launch overlapping preflight
  // probes on every two-second status tick while the previous request waits.
  if (refreshInFlight) return;
  refreshInFlight = true;
  try {
    if (!config || Date.now() - configRefreshedAt >= 60000) {
      config = await readJSON(await fetch('/api/stress/config', {cache: 'no-store'}));
      configRefreshedAt = Date.now();
    }
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
    const failure = refreshFailure(error);
    byId('policy').textContent = failure.message;
    byId('policy-state').textContent = failure.label;
    byId('policy-state').className = 'policy-badge blocked';
    byId('start').disabled = true;
    setError(failure.message);
  } finally {
    refreshInFlight = false;
  }
}

async function start() {
  setError('');
  const benchmarks = Array.from(selected || []);
  if (!benchmarks.length) {
    setError('请至少选择一个已就绪的可靠性压测项目。');
    return;
  }
  const payload = {benchmarks};
  if (benchmarks.includes('npu_burn')) {
    payload.npu_devices = Array.from(selectedNPUDevices || []);
    if (!payload.npu_devices.length) { setError('请明确选择至少一个 NPU 逻辑设备。'); return; }
    const npu = (config.benchmarks || []).find(b => b.name === 'npu_burn');
    if (npuMode === 'model') {
      payload.npu_model = {name:selectedNPUModel, action:npuModelAction, reference_id:byId('npu-reference-id').value.trim()};
      if (!payload.npu_model.name || !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(payload.npu_model.reference_id)) { setError('请选择已授权模型并填写有效参考 ID。'); return; }
      if (payload.npu_model.action === 'create_reference' && payload.npu_devices.length !== 1) { setError('生成参考时只能选择一台逻辑设备。'); return; }
    } else if (profileParameters(npu.profile || {}).case_catalog) {
      payload.npu_cases = Array.from(selectedNPUCases || []);
      if (!payload.npu_cases.length) { setError('请明确选择至少一个 NPU 算子。'); return; }
    }
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
        '，配置 ' + ((timeoutSeconds || item.name === 'npu_burn') ? '提交后生成新哈希' : shortHash(profile.configuration_sha256)) +
        (profile.preflight && profile.preflight.status === 'warn' ? ' [预检警告]' : '');
    });
  if (payload.npu_devices) profileLines.push('本次 NPU logical ID：' + payload.npu_devices.join(','));
  if (payload.npu_cases) profileLines.push('本次 NPU 算子：' + payload.npu_cases.join(','));
  if (payload.npu_model) {
    profileLines.push('本次模型：' + payload.npu_model.name);
    profileLines.push('本次任务：' + (payload.npu_model.action === 'create_reference' ? '生成参考（不是 SDC 检测通过）' : 'SDC 对照检测'));
    profileLines.push('参考 ID：' + payload.npu_model.reference_id);
  }
  if (!window.confirm(
    '将启动高负载可靠性压测：\n\n' + profileLines.join('\n') +
    '\n\n设备将固定到本次作业；Controller 会重新验证权限、拓扑与运行条件。确认执行吗？'
  )) return;
  try {
    report = await readJSON(await fetch('/api/stress/runs', {
      method: 'POST',
      headers: {'Content-Type': 'application/json', 'X-CATMonitor-Action': 'stress'},
      body: JSON.stringify({...payload, timeout_seconds: timeoutSeconds})
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

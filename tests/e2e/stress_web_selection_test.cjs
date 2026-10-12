// No browser, server, Docker or NPU required. Exercise the actual Web script's
// selection/submission contract, not a reimplementation of its functions.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
function element(tagName = 'div') {
  return {tagName, children: [], value: '', textContent: '', hidden: false, disabled: false, style: {},
    attributes: {}, classList: {add() {}},
    querySelector(selector) { if (!this.selectors) this.selectors = {}; if (!this.selectors[selector]) this.selectors[selector] = element('option'); return this.selectors[selector]; },
    setAttribute(name, value) { this.attributes[name] = value; },
    get innerHTML() { return ''; },
    set innerHTML(value) { this.children = []; },
    addEventListener(event, fn) { this[event] = fn; },
    appendChild(value) { this.children.push(value); return value; },
    append(...values) { this.children.push(...values); },
    replaceChildren(...values) { this.children = values; }};
}
const nodes = new Map();
const document = {getElementById(id) {
  if (!nodes.has(id)) nodes.set(id, element());
  return nodes.get(id);
}, createElement: element, createTextNode: text => text};
const requests = [];
function allText(node) {
  return typeof node === 'string' ? node : [node.textContent, ...node.children.map(allText)].join(' ');
}
function descendants(node) {
  return [node, ...node.children.filter(child => typeof child !== 'string').flatMap(descendants)];
}
const context = vm.createContext({document, console,
  window: {clearTimeout() {}, addEventListener() {}, confirm: () => true},
  setTimeout() {}, setInterval() {}, fetch: () => new Promise(() => {})});
vm.runInContext(fs.readFileSync(path.join(__dirname, '../../features/stress/static/stress.js'), 'utf8'), context);
context.testFetch = async (url, options) => {
  requests.push({url, ...options});
  return {ok: true, json: async () => ({job_id: 'aabb', status: 'running'})};
};
vm.runInContext(`
  fetch = testFetch;
  globalThis.testRenderReport = renderReport;
  renderConfig = renderHistory = renderReport = () => {};
  setError = value => { globalThis.lastError = value; };
  config = {benchmarks: [{name:'npu_burn', timeout_seconds:60, profile:{
    timeout_seconds:60, parameters:[
      {key:'devices', value:'1'},
      {key:'available_devices', value:'0,1,2'},
      {key:'allowed_devices', value:'0,1'},
      {key:'selected_cases', value:'matmul'},
      {key:'case_catalog', value:JSON.stringify([
        {name:'matmul',selectable:true},{name:'conv2d',selectable:true},
        {name:'quant_matmul',selectable:false,reason:'no upstream A2 QuantMatmul strategy'},
        {name:'gpt3',selectable:false,reason:'model requires a separately prepared SDC reference'},
        {name:'llama2',selectable:true}])}]}}]};
  selected = new Set(['npu_burn']);
  renderNPUSelection();
`, context);
assert.equal(nodes.get('npu-selection').hidden, false);
assert.equal(nodes.get('npu-device-choices').children.length, 3);
assert.equal(nodes.get('npu-device-choices').children[2].children[0].disabled, true);
assert.equal(vm.runInContext('Array.from(selectedNPUDevices).join(",")', context), '1');
assert.equal(nodes.get('npu-case-choices').children.length, 2, 'only admitted operators are selectable');
assert.equal(nodes.get('npu-case-count').textContent, '已选 1 项');
assert.doesNotMatch(allText(nodes.get('npu-case-choices')), /待验证|实机|不支持|quant_matmul|gpt3|llama2/);
assert.match(allText(nodes.get('npu-unavailable-items')), /quant_matmul.*上游未提供 A2/);
assert.match(allText(nodes.get('npu-unavailable-items')), /gpt3.*SDC 参考数据/);
assert.equal(nodes.get('npu-unavailable-items').children.length, 3);
assert.match(nodes.get('npu-case-hint').textContent, /实际运行结果以作业报告为准/);
const html = fs.readFileSync(path.join(__dirname, '../../features/stress/static/index.html'), 'utf8');
assert.match(html, /<details id="profile-preview"[^>]*>/, 'deployment details are collapsed by default');
assert.doesNotMatch(html, /<details id="profile-preview"[^>]*\bopen\b/);
assert.match(html, /<details class="panel history-panel">/);
assert.doesNotMatch(html, /能力覆盖|运行成功即通过|hero-visual/);
assert.match(html, /压测会占用计算资源/);
const conv = nodes.get('npu-case-choices').children[1].children[0];
conv.checked = true;
conv.change();
assert.equal(nodes.get('npu-case-count').textContent, '已选 2 项');
nodes.get('npu-select-all').onclick();
assert.equal(vm.runInContext('Array.from(selectedNPUDevices).join(",")', context), '0,1');
vm.runInContext(`
  globalThis.originalCatalog = config.benchmarks[0].profile.parameters[4].value;
  config.benchmarks[0].profile.parameters[4].value = JSON.stringify(
    ['matmul','conv2d','permute','fusion_attention','matmul_atomic','matmul_sdma','matmul_axpy','conv3d']
      .map(name=>({name,selectable:true})));
  renderNPUSelection();
`, context);
const moreOperators = nodes.get('npu-case-choices').children.find(node=>node.tagName==='details');
assert.equal(nodes.get('npu-case-choices').children.length, 7, 'six common operators plus collapsed additional operators');
assert.equal(moreOperators.open, false);
assert.match(moreOperators.children[0].textContent, /更多算子（2 项）/);
const extraOperator = moreOperators.children[1].children[0].children[0];
extraOperator.checked = true;
extraOperator.change();
assert.equal(nodes.get('npu-case-count').textContent, '已选 3 项');
assert.match(allText(nodes.get('npu-case-choices')), /更多算子（2 项）.*已选 1 项/);
vm.runInContext(`
  config.benchmarks[0].profile.parameters[4].value = originalCatalog;
  selectedNPUCases = new Set(['matmul','conv2d']); renderNPUSelection();
`, context);

// Result counts come from this report's CSV groups, never current selectors
// or a division of aggregate totals. Older reports remain honest/readable.
vm.runInContext(`
  globalThis.testGroups = [
    {device:0,operator:'matmul',cases:1,passed:1,failed:0,errors:0,case_time_seconds:1},
    {device:0,operator:'conv2d',cases:6,passed:6,failed:0,errors:0,case_time_seconds:10},
    {device:1,operator:'matmul',cases:1,passed:1,failed:0,errors:0,case_time_seconds:1},
    {device:1,operator:'conv2d',cases:6,passed:6,failed:0,errors:0,case_time_seconds:10.626}
  ];
  globalThis.testItem = {name:'npu_burn',status:'healthy',duration_ms:55000,
    values:{cases:14,passed:14,failed:0,errors:0,case_time_seconds:22.626},
    profile:{parameters:[],resources:{},assets:[],timeout_seconds:60,configuration_sha256:'hash'},
    npu_selection:{deployment_default:[0],requested:[0,1],selected:[0,1],actual:[0,1],
      default_cases:['matmul'],requested_cases:['matmul','conv2d'],selected_cases:['matmul','conv2d'],actual_cases:['matmul','conv2d']},
    output:'CATMONITOR_NPU_BURN_CASE_GROUPS='+JSON.stringify(testGroups)+'\\nCATMONITOR_NPU_BURN_RESULT_CSV=/results/current.csv'};
  report = {job_id:'csv-job',status:'healthy',started_at:'2026-10-12T07:00:00Z',finished_at:'2026-10-12T07:01:00Z',benchmarks:[testItem]};
  testRenderReport();
`, context);
let card = nodes.get('result-cards').children[0];
const mainText = card.children.filter(node => node.tagName !== 'details').map(allText).join(' ');
assert.match(mainText, /设备：logical 0、logical 1/);
assert.match(mainText, /算子：matmul、conv2d/);
assert.equal((mainText.match(/14 \/ 14/g) || []).length, 1, 'pass total must appear only once');
assert.doesNotMatch(mainText, /部署默认|请求|固定|累计用例时间|hash|current.csv/);
assert.doesNotMatch(nodes.get('meta').textContent, /csv-job|配置|hash/, 'technical identifiers stay out of the report headline');
assert.match(nodes.get('meta').title, /csv-job/);
const groupsDetail = card.children.find(node => node.tagName === 'details');
assert.equal(groupsDetail.open, false);
assert.match(allText(groupsDetail), /CSV 结果记录数，不是算子数、设备数/);
const table = descendants(groupsDetail).find(node => node.tagName === 'table');
assert.equal(table.children[1].children.length, 4);
assert.match(allText(table), /6 \/ 6/);
assert.match(mainText, /作业耗时：1 分钟/);
assert.match(allText(card), /累计用例时间.*22.626.*不含提交前准入检查/);
assert.match(allText(card), /current.csv/);
vm.runInContext(`testItem.npu_csv={path:'/results/current.csv',groups:testGroups}; testItem.output=''; testRenderReport()`, context);
assert.equal(descendants(nodes.get('result-cards')).find(node => node.tagName === 'tbody').children.length, 4, 'history metadata survives output removal');
assert.match(allText(nodes.get('result-cards')), /current.csv/);
vm.runInContext(`delete testItem.npu_csv;
  testItem.output='CATMONITOR_NPU_BURN_CASE_GROUPS='+JSON.stringify(testGroups);`, context);
vm.runInContext(`testItem.npu_selection.actual=[0]; testItem.npu_selection.actual_cases=['matmul']; testRenderReport()`, context);
card = nodes.get('result-cards').children[0];
const warning = card.children.find(node => node.className === 'npu-result-warning');
assert.equal(warning.attributes.role, 'alert');
assert.match(warning.textContent, /设备不一致.*算子不一致/);
// A union of device/operator names is insufficient: every selected pair must
// have CSV results, including when aggregate counts remain self-consistent.
assert.match(vm.runInContext(`npuResultIdentity(testItem, testGroups.filter(g=>!(g.device===1&&g.operator==='conv2d'))).differences.join(';')`, context), /CSV 缺少 logical 1 \/ conv2d/);
vm.runInContext(`testItem.npu_selection.requested=[0]; testItem.npu_selection.requested_cases=['matmul']; testRenderReport()`, context);
assert.match(allText(nodes.get('result-cards')), /请求设备与本次固定设备不一致.*请求算子与本次固定算子不一致/);
vm.runInContext(`testItem.npu_selection.requested=[0,1]; testItem.npu_selection.requested_cases=['matmul','conv2d'];`, context);
vm.runInContext(`testItem.npu_selection.actual=[0,1]; testItem.npu_selection.actual_cases=['matmul','conv2d'];
  testItem.output=''; testRenderReport()`, context);
assert.match(allText(nodes.get('result-cards')), /未保存 CSV 分组明细/);
assert.equal(descendants(nodes.get('result-cards')).filter(node => node.tagName === 'table').length, 0);
vm.runInContext(`testGroups[0].passed=0;
  testItem.output='CATMONITOR_NPU_BURN_CASE_GROUPS='+JSON.stringify(testGroups); testRenderReport()`, context);
assert.match(allText(nodes.get('result-cards')), /CSV 分组数据无效或与报告总数不一致/);
vm.runInContext(`testGroups[0].passed=1; testGroups[0].operator='<img src=x onerror=alert(1)>';
  testItem.output='CATMONITOR_NPU_BURN_CASE_GROUPS='+JSON.stringify(testGroups); testRenderReport()`, context);
assert.equal(descendants(nodes.get('result-cards')).filter(node => node.tagName === 'img').length, 0, 'CSV names are text, not HTML');
vm.runInContext(`testItem.status='cancelled'; testItem.values={}; testItem.output='';
  delete testItem.npu_selection.actual; delete testItem.npu_selection.actual_cases; testRenderReport()`, context);
assert.match(allText(nodes.get('result-cards')), /尚无完整 CSV 用例结果，不能认定通过/);
assert.doesNotMatch(allText(nodes.get('result-cards')), /14 \/ 14|检测错误 0/);
vm.runInContext(`report=null;`, context);
(async () => {
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 1);
  assert.deepEqual(JSON.parse(requests[0].body).npu_devices, [0, 1]);
  assert.deepEqual(JSON.parse(requests[0].body).npu_cases, ['matmul', 'conv2d']);
  assert.equal(requests[0].headers['X-CATMonitor-Action'], 'stress');
  vm.runInContext('selectedNPUDevices = new Set(); report = null;', context);
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 1, 'empty selection must not submit all');
  assert.match(context.lastError, /至少一个/);
  vm.runInContext('selectedNPUDevices = new Set([0]); selectedNPUCases = new Set();', context);
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 1, 'empty operator selection must not submit');
  assert.match(context.lastError, /NPU 算子/);
  // Polling/config changes remove no-longer-authorized selections, never
  // silently expand to all newly visible devices.
  vm.runInContext(`selectedNPUDevices = new Set([1]);
    config.benchmarks[0].profile.parameters[2].value = '0';
    renderNPUSelection();`, context);
  assert.equal(vm.runInContext('selectedNPUDevices.size', context), 0);
  vm.runInContext("report = {job_id:'aabb', status:'running'}; selectedNPUDevices = new Set([0]);", context);
  await vm.runInContext('cancel()', context);
  assert.equal(requests[1].url, '/api/stress/runs/aabb/cancel', 'cancel must use job ID, not current selection');
  vm.runInContext(`
    report=null;
    config.benchmarks[0].profile.parameters.push({key:'allowed_models',value:'gpt3,llama2'}, {key:'allow_reference_generation',value:'true'});
    selectedNPUDevices = new Set([0]);
    renderNPUSelection();
    byId('npu-mode').value='model'; byId('npu-mode').onchange();
    byId('npu-model-name').value='gpt3';
    byId('npu-model-action').value='create_reference';
    byId('npu-reference-id').value='ref1';
  `, context);
  assert.equal(nodes.get('npu-model-selection').hidden, false);
  await vm.runInContext('start()', context);
  assert.deepEqual(JSON.parse(requests[2].body).npu_model, {name:'gpt3',action:'create_reference',reference_id:'ref1'});
  assert.equal(JSON.parse(requests[2].body).npu_cases, undefined, 'models are not operator requests');
  vm.runInContext('report=null; selectedNPUDevices = new Set([0,1]);', context);
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 3, 'multiple reference generators must be rejected');
  assert.match(context.lastError, /只能选择一台/);
  vm.runInContext(`byId('npu-model-action').value='compare';`, context);
  await vm.runInContext('start()', context);
  assert.deepEqual(JSON.parse(requests[3].body).npu_devices, [0,1]);
  vm.runInContext(`testItem.status='healthy'; testItem.npu_model={model:'gpt3',action:'create_reference',reference_id:'ref1',devices:[0],outcome:'reference_generated',checked_steps:0,saved_steps:3,errors:0,csv:'/results/model.csv'}; report={job_id:'model-job',status:'healthy',benchmarks:[testItem]}; testRenderReport()`, context);
  assert.match(allText(nodes.get('result-cards')), /参考生成成功（不是 SDC 检测通过）/);
  vm.runInContext(`testItem.status='unhealthy'; testRenderReport()`,context);
  assert.doesNotMatch(allText(nodes.get('result-cards')), /参考生成成功|模型 SDC 对照通过/);
  vm.runInContext(`testItem.failure={code:'npu_hbm_oom',resource:'npu_hbm'};
    testItem.npu_selection={selected:[0],model:{name:'gpt3',action:'create_reference',reference_id:'ref1'}};
    testItem.output='RuntimeError: NPU out of memory'; testRenderReport()`,context);
  assert.match(allText(nodes.get('result-cards')), /显存不足 · 未完成/);
  assert.match(allText(nodes.get('result-cards')), /不等于设备硬件故障/);
  assert.doesNotMatch(allText(nodes.get('result-cards')), /参考生成成功|模型 SDC 对照通过|压测通过/);
  vm.runInContext(`delete testItem.failure; testItem.status='healthy';
    testItem.npu_model.outcome='sdc_passed'; testItem.npu_model.memory_mode='checkpointed'; testRenderReport()`,context);
  assert.match(allText(nodes.get('result-cards')), /压测通过/);
  assert.match(allText(nodes.get('result-cards')), /模型 SDC 对照通过/);
  assert.match(allText(nodes.get('result-cards')), /完整参数 · 激活重计算/);
  console.log('PASS: Web selection, CSV grouping/mismatch/legacy report, request and Job-ID cancel (DOM fixture)');
})().catch(error => { console.error(error); process.exitCode = 1; });

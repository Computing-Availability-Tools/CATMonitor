// No browser, server, Docker or NPU required. Exercise the actual Web script's
// selection/submission contract, not a reimplementation of its functions.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
function element(tagName = 'div') {
  const node = {tagName, children: [], value: '', textContent: '', hidden: false, disabled: false, style: {},
    attributes: {}, classList: {add() {}},
    querySelector(selector) { if (!this.selectors) this.selectors = {}; if (!this.selectors[selector]) this.selectors[selector] = element('option'); return this.selectors[selector]; },
    setAttribute(name, value) { this.attributes[name] = value; },
    get innerHTML() { return ''; },
    set innerHTML(value) { this.children = []; },
    addEventListener(event, fn) { this[event] = fn; },
    appendChild(value) { this.children.push(value); return value; },
    append(...values) { this.children.push(...values); },
    replaceChildren(...values) { this.children = values; }};
  // HTMLTextAreaElement.type is getter-only in browsers. A writable generic
  // object used to hide the real NPU tensor editor crash from this fixture.
  if (tagName === 'textarea') Object.defineProperty(node, 'type', {get() { return 'textarea'; }});
  return node;
}
const nodes = new Map();
const document = {getElementById(id) {
  if (!nodes.has(id)) nodes.set(id, element());
  return nodes.get(id);
}, createElement: element, createTextNode: text => text};
const requests = [];
const confirmations = [];
function allText(node) {
  return typeof node === 'string' ? node : [node.textContent, ...node.children.map(allText)].join(' ');
}
function descendants(node) {
  return [node, ...node.children.filter(child => typeof child !== 'string').flatMap(descendants)];
}
const context = vm.createContext({document, console,
  window: {clearTimeout() {}, addEventListener() {}, confirm: message => { confirmations.push(message); return true; }},
  setTimeout() {}, setInterval() {}, fetch: () => new Promise(() => {})});
let pendingRefreshRequests = 0;
const refreshContext = vm.createContext({document, console,
  window: {clearTimeout() {}, addEventListener() {}}, setTimeout() {}, setInterval() {},
  fetch: () => { pendingRefreshRequests++; return new Promise(() => {}); }});
vm.runInContext(fs.readFileSync(path.join(__dirname, '../../features/stress/static/stress.js'), 'utf8'), refreshContext);
vm.runInContext('refresh(); refresh();', refreshContext);
assert.equal(pendingRefreshRequests, 1, 'pending NPU preflight must not overlap on polling ticks');
vm.runInContext(fs.readFileSync(path.join(__dirname, '../../features/stress/static/stress.js'), 'utf8'), context);
context.testFetch = async (url, options) => {
  requests.push({url, ...options});
  return {ok: true, json: async () => ({job_id: 'aabb', status: 'running'})};
};
vm.runInContext(`
  fetch = testFetch;
  globalThis.testRenderReport = renderReport;
  globalThis.testRenderConfig = renderConfig;
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
assert.doesNotMatch(allText(nodes.get('npu-unavailable-items')), /gpt3|llama2|SDC 参考数据/);
assert.equal(nodes.get('npu-unavailable-items').children.length, 1);
vm.runInContext(`{
  const list=JSON.parse(config.benchmarks[0].profile.parameters[4].value);
  list.find(c=>c.name==='matmul').config={shape:[[[4,8],[8,16]]],dtype:['float16'],pattern:['gauss_random'],run_count:13};
  list.find(c=>c.name==='conv2d').config={input_data:[1,2,3,4],weight_data:[4,2,3,3],dtype:['float32'],pattern:['uniform_random'],run_count:29};
  config.benchmarks[0].profile.parameters[4].value=JSON.stringify(list); renderNPUSelection();
}`,context);
assert.equal(nodes.get('npu-operator-parameter-items').children.length,1,'only selected operators have expanded details');
let opDetail=nodes.get('npu-operator-parameter-items').children[0];
assert.equal(opDetail.open,false,'tensor details must be collapsed by default');
assert.match(allText(opDetail),/matmul.*Tensor shape.*4,8.*dtype.*float16.*pattern.*gauss_random.*run_count.*13/);
assert.equal(descendants(opDetail).some(node=>['input','select','textarea'].includes(node.tagName)),false,'parameters are readonly');
assert.match(nodes.get('npu-case-hint').textContent, /实际运行结果以作业报告为准/);
const html = fs.readFileSync(path.join(__dirname, '../../features/stress/static/index.html'), 'utf8');
assert.match(html, /<details id="profile-preview"[^>]*>/, 'deployment details are collapsed by default');
assert.doesNotMatch(html, /<details id="npu-operator-parameters"[^>]*\bopen\b/);
assert.doesNotMatch(html, /<details id="profile-preview"[^>]*\bopen\b/);
assert.match(html, /<details class="panel history-panel">/);
assert.match(html, /能力覆盖.*[\s\S]*执行完成，校验通过/);
assert.doesNotMatch(html, /运行成功即通过/);
assert.match(html, /STREAM · Copy \/ Scale \/ Add \/ Triad/);
assert.match(html, /NPU 算子必须生成本轮完整 PASS\/SDC 结果且错误数为 0/);
assert.match(html, /模型必须完成与匹配参考的对照；生成参考不等于检测通过/);
assert.doesNotMatch(html, /<select id="npu-mode"|<select id="npu-model-name"|<select id="npu-model-action"|hero-visual/);
assert.match(html, /id="npu-mode-model"[^>]*>模型 SDC/);
assert.match(html, /首次使用：准备参考/);
assert.doesNotMatch(html, /<details id="npu-reference-setup"[^>]*\bopen\b/);
vm.runInContext(`{
  const oldConfig = config, oldSelected = selected, oldReport = report;
  config = {enabled:true, platform:'linux', feature_enabled:true, web_enabled:true, shared_report:true,
    benchmarks:['npu_burn','hpl','hpcg','stream'].map(name=>({name,enabled:true,available:true,timeout_seconds:60}))};
  selected = new Set(); report = null; testRenderConfig();
  config = oldConfig; selected = oldSelected; report = oldReport;
}`, context);
assert.deepEqual(nodes.get('choices').children.map(node=>node.children[0].value), ['hpcg','hpl','stream','npu_burn']);
assert.equal(nodes.get('npu-mode-model').disabled, true);
assert.match(html, /压测会占用计算资源/);
const conv = nodes.get('npu-case-choices').children[1].children[0];
conv.checked = true;
conv.change();
assert.equal(nodes.get('npu-case-count').textContent, '已选 2 项');
assert.equal(nodes.get('npu-operator-parameter-items').children.length,2);
assert.match(allText(nodes.get('npu-operator-parameter-items')),/conv2d.*input_data.*weight_data.*run_count.*29/);
nodes.get('npu-select-all').onclick();
assert.equal(vm.runInContext('Array.from(selectedNPUDevices).join(",")', context), '0,1');
vm.runInContext(`
  globalThis.originalCatalog = config.benchmarks[0].profile.parameters[4].value;
  config.benchmarks[0].profile.parameters[4].value = JSON.stringify(
    ['matmul','conv2d','permute','fusion_attention','matmul_atomic','matmul_sdma','matmul_axpy','conv3d']
      .map(name=>({name,selectable:true})));
  renderNPUSelection();
`, context);
assert.equal(nodes.get('npu-case-choices').children.length, 8, 'every admitted operator is directly visible');
assert.equal(nodes.get('npu-case-choices').children.some(node=>node.tagName==='details'), false);
const extraOperator = nodes.get('npu-case-choices').children[6].children[0];
extraOperator.checked = true;
extraOperator.change();
assert.equal(nodes.get('npu-case-count').textContent, '已选 3 项');
assert.match(allText(nodes.get('npu-case-choices')), /matmul_axpy.*conv3d/);
assert.doesNotMatch(allText(nodes.get('npu-case-choices')), /更多算子/);
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
vm.runInContext(`testItem.npu_parameters={schema_version:1,job_id:'csv-job',chip_generation:'A2',operators:[
  {name:'matmul',config:{shape:[[[4,8],[8,16]]],dtype:['float16'],pattern:['gauss_random'],run_count:13}},
  {name:'conv2d',config:{input_data:[1,2,3,4],weight_data:[4,2,3,3],dtype:['float32'],pattern:['uniform_random'],run_count:29}}]};
  testRenderReport();`,context);
let actualDetails=descendants(nodes.get('result-cards')).filter(node=>String(node.className).includes('npu-operator-parameters'));
assert.equal(actualDetails.length,2);
assert(actualDetails.every(node=>node.open===false));
assert.match(allText(actualDetails[0]),/执行端实际加载.*run_count.*13/);
vm.runInContext(`{
  const list=JSON.parse(config.benchmarks[0].profile.parameters[4].value);
  list.find(c=>c.name==='matmul').config.run_count=999;
  config.benchmarks[0].profile.parameters[4].value=JSON.stringify(list); renderNPUSelection(); testRenderReport();
}`,context);
actualDetails=descendants(nodes.get('result-cards')).filter(node=>String(node.className).includes('npu-operator-parameters'));
assert.match(allText(actualDetails[0]),/run_count.*13/);
assert.doesNotMatch(allText(actualDetails[0]),/999/,'history must not query the current catalog');
vm.runInContext(`testItem.npu_csv={path:'/results/current.csv',groups:testGroups}; testItem.output=''; testRenderReport()`, context);
assert.equal(descendants(nodes.get('result-cards')).find(node => node.tagName === 'tbody').children.length, 4, 'history metadata survives output removal');
assert.equal(descendants(nodes.get('result-cards')).filter(node=>String(node.className).includes('npu-operator-parameters')).length,2,'actual parameters survive removed console output');
assert.match(allText(nodes.get('result-cards')), /current.csv/);
vm.runInContext(`delete testItem.npu_csv;
  testItem.output='CATMONITOR_NPU_BURN_CASE_GROUPS='+JSON.stringify(testGroups);`, context);
vm.runInContext(`testItem.npu_parameters.job_id='another-job'; testRenderReport();`,context);
assert.equal(descendants(nodes.get('result-cards')).filter(node=>String(node.className).includes('npu-operator-parameters')).length,0);
assert.match(allText(nodes.get('result-cards')),/未保存有效的本次算子参数快照/);
vm.runInContext(`delete testItem.npu_parameters; testRenderReport();`,context);
assert.match(allText(nodes.get('result-cards')),/不以当前配置或启动前预检值代替/,'older reports must not fabricate tensor data');
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
  const lostConnection = vm.runInContext('refreshFailure(new TypeError("Failed to fetch"))', context);
  assert.equal(lostConnection.label, '连接中断');
  assert.match(lostConnection.message, /SSH.*隧道/);
  assert.doesNotMatch(lostConnection.message, /预检失败|算子失败/);
  assert.equal(vm.runInContext('refreshFailure(new TypeError("Cannot read properties of undefined"))', context).label,
    '读取失败', 'a rendering/schema error is not a disconnected network');
  const controllerError = vm.runInContext('refreshFailure(new Error("HTTP 503"))', context);
  assert.equal(controllerError.label, '读取失败');
  assert.match(controllerError.message, /HTTP 503/);
  context.invalidJSON = {ok: true, json: async () => { throw new SyntaxError('Unexpected token <'); }};
  await assert.rejects(vm.runInContext('readJSON(invalidJSON)', context), /有效 JSON/);
  const failureNodes = new Map();
  const failureContext = vm.createContext({console,
    document: {getElementById(id) { if (!failureNodes.has(id)) failureNodes.set(id, element()); return failureNodes.get(id); }, createElement: element, createTextNode: text => text},
    window: {clearTimeout() {}, addEventListener() {}}, setTimeout() {}, setInterval() {},
    fetch: async () => { throw new TypeError('Failed to fetch'); }});
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../../features/stress/static/stress.js'), 'utf8'), failureContext);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(failureNodes.get('policy-state').textContent, '连接中断');
  assert.equal(failureNodes.get('start').disabled, true, 'lost connectivity must disable new operator requests');
  failureContext.fetch = async url => ({ok: true, json: async () => url.includes('/config')
    ? {enabled:true, benchmarks:[{name:'stream', enabled:true, available:true, timeout_seconds:60}], default_benchmarks:['stream']}
    : url.includes('/history') ? [] : null});
  await vm.runInContext('refresh()', failureContext);
  assert.notEqual(failureNodes.get('policy-state').textContent, '连接中断');
  assert.equal(failureNodes.get('start').disabled, false, 'a successful refresh must restore the Run control');
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
    config.benchmarks[0].profile.parameters[2].value='0,1';
    selectedNPUDevices = new Set([0]);
    renderNPUSelection();
    byId('npu-mode-model').onclick();
  `, context);
  assert.equal(nodes.get('npu-model-selection').hidden, false);
  assert.equal(nodes.get('npu-mode-model').disabled, false);
  assert.equal(nodes.get('npu-mode-model').attributes['aria-pressed'], 'true');
  assert.equal(nodes.get('npu-case-choices').hidden, true);
  assert.equal(nodes.get('npu-model-choices').children.length, 2);
  assert.equal(nodes.get('npu-model-choices').children[0].textContent, 'GPT3');
  assert.equal(nodes.get('npu-model-choices').children[1].textContent, 'Llama2');
  assert.equal(vm.runInContext('npuModelAction', context), 'compare', 'SDC detection is the default, never automatic reference generation');
  assert.equal(nodes.get('start-label').textContent, '开始 SDC 检测');
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 2, 'a missing reference must not create one implicitly');
  assert.match(context.lastError, /参考 ID/);
  vm.runInContext(`byId('npu-reference-id').value='ref1';`, context);
  await vm.runInContext('start()', context);
  assert.deepEqual(JSON.parse(requests[2].body).npu_model, {name:'gpt3',action:'compare',reference_id:'ref1'});
  assert.equal(JSON.parse(requests[2].body).npu_cases, undefined, 'models are not operator requests');
  assert.match(confirmations.at(-1), /SDC 对照检测.*\n参考 ID：ref1/);
  vm.runInContext(`report=null; byId('npu-model-prepare').onclick();`, context);
  assert.equal(nodes.get('npu-model-preparing').hidden, false);
  assert.equal(nodes.get('start-label').textContent, '生成参考');
  await vm.runInContext('start()', context);
  assert.deepEqual(JSON.parse(requests[3].body).npu_model, {name:'gpt3',action:'create_reference',reference_id:'ref1'});
  assert.match(confirmations.at(-1), /生成参考（不是 SDC 检测通过）/);
  vm.runInContext('report=null; selectedNPUDevices = new Set([0,1]);', context);
  await vm.runInContext('start()', context);
  assert.equal(requests.length, 4, 'multiple reference generators must be rejected');
  assert.match(context.lastError, /只能选择一台/);
  vm.runInContext(`byId('npu-model-compare').onclick(); byId('npu-model-choices').children[1].onclick();`, context);
  assert.equal(nodes.get('npu-model-choices').children[1].attributes['aria-pressed'], 'true');
  await vm.runInContext('start()', context);
  assert.deepEqual(JSON.parse(requests[4].body).npu_devices, [0,1]);
  assert.deepEqual(JSON.parse(requests[4].body).npu_model, {name:'llama2',action:'compare',reference_id:'ref1'});
  vm.runInContext(`report=null; selectedNPUDevices=new Set([0]); byId('npu-model-prepare').onclick();
    config.benchmarks[0].profile.parameters.find(p=>p.key==='allow_reference_generation').value='false';
    renderNPUSelection(); byId('npu-model-prepare').onclick();`, context);
  assert.equal(nodes.get('npu-model-prepare').disabled, true);
  assert.equal(vm.runInContext('npuModelAction',context), 'compare', 'revoked generation permission must fail closed');
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
  await require('./stress_web_model_preflight_test.cjs')();
  vm.runInContext(`
    report=null; selected=new Set(['stream']); jobParameterDraft.clear(); jobParameterSelectionKey='';
    config={benchmarks:[{name:'stream',timeout_seconds:60,profile:{frozen_parameters:{effective:{stream:{threads:4}}}}}]};
    renderJobParameters();
  `,context);
  assert.doesNotMatch(html,/<details id="job-parameters"[^>]*\bopen\b/,'advanced edits are collapsed by default');
  assert.equal(vm.runInContext('currentJobParameters()',context),undefined,'blank inherits without sending default values');
  assert.equal(vm.runInContext('jobParameterFields[0].control.placeholder',context),'4');
  vm.runInContext(`jobParameterFields[0].control.value='2'; jobParameterFields[0].control.input(); renderJobParameters();`,context);
  await vm.runInContext('start()',context);
  assert.deepEqual(JSON.parse(requests.at(-1).body).parameters,{stream:{threads:2}});
  vm.runInContext(`report=null; jobParameterFields[0].control.value='1.5';`,context);
  const beforeInvalid=requests.length; await vm.runInContext('start()',context);
  assert.equal(requests.length,beforeInvalid,'invalid editor input must not start a job');
  vm.runInContext(`
    jobParameterDraft.clear(); jobParameterSelectionKey=''; selected=new Set(['npu_burn']); selectedNPUCases=new Set(['matmul']);npuMode='operators';
    config={benchmarks:[{name:'npu_burn',profile:{frozen_parameters:{effective:{npu_burn:{}}},parameters:[{key:'case_catalog',value:JSON.stringify([{name:'matmul',config:{dtype:['float16'],pattern:['gauss_random'],shape:[[[4,8],[8,16]]],run_count:3}},{name:'gpt3',config:{batch_size:1,hidden_size:32}}])}]}}]};
    renderJobParameters();
    jobParameterFields.find(f=>f.path.endsWith('.shape')).control.value='[[[16,8],[8,32]]]';
  `,context);
  assert.deepEqual(JSON.parse(vm.runInContext('JSON.stringify(currentJobParameters())',context)),{npu_burn:{operators:{matmul:{shape:[[[16,8],[8,32]]]}}}});
  assert.equal(vm.runInContext("jobParameterFields.find(f=>f.path.endsWith('.shape')).control.type", context), 'textarea');
  assert.equal(vm.runInContext("jobParameterFields.find(f=>f.path.endsWith('.run_count')).control.type", context), 'number', 'textarea must not prevent later run_count fields from rendering');
  vm.runInContext(`
    selectedNPUCases.add('conv2d');
    const catalog=JSON.parse(config.benchmarks[0].profile.parameters[0].value);
    catalog.push({name:'conv2d',config:{dtype:['float16'],pattern:['uniform_random'],input_data:[1,2,16,16],weight_data:[4,2,3,3],run_count:2}});
    config.benchmarks[0].profile.parameters[0].value=JSON.stringify(catalog);
    renderJobParameters();
    const dimensions=jobParameterFields.find(f=>f.path==='npu_burn.operators.conv2d.dimensions');
    dimensions.control.value='{"input_data":[1,2,8,8],"weight_data":[4,2,3,3]}'; dimensions.control.input();
    const count=jobParameterFields.find(f=>f.path==='npu_burn.operators.conv2d.run_count');
    count.control.value='5'; count.control.input();
  `, context);
  assert.deepEqual(JSON.parse(vm.runInContext('JSON.stringify(currentJobParameters())',context)),{npu_burn:{operators:{conv2d:{dimensions:{input_data:[1,2,8,8],weight_data:[4,2,3,3]},run_count:5}}}}, 'multi-operator dimensions and run_count remain editable with readonly textarea.type');
  vm.runInContext(`jobParameterSelectionKey=''; renderJobParameters();`, context);
  assert.equal(vm.runInContext("jobParameterFields.find(f=>f.path==='npu_burn.operators.conv2d.run_count').control.value", context), '5', 'rerender preserves this job draft');
  vm.runInContext(`npuMode='model';selectedNPUModel='gpt3';renderJobParameters();jobParameterFields.find(f=>f.path==='npu_burn.model.batch_size').control.value='2';`,context);
  assert.deepEqual(JSON.parse(vm.runInContext('JSON.stringify(currentModelPreflightRequest().parameters)',context)),{npu_burn:{model:{batch_size:2}}},'model preflight uses the same edited request, not stale operator values');
  console.log('PASS: per-job Web editor inheritance, typed submission and model preflight configuration');
  console.log('PASS: Web selection, CSV grouping/mismatch/legacy report, request and Job-ID cancel (DOM fixture)');
})().catch(error => { console.error(error); process.exitCode = 1; });

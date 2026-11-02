// Run the real browser script with synthetic capabilities/preflight responses.
// No Docker, model training, reference creation or hardware certification.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
function element(tagName = 'div') {
  return {tagName, children: [], value: '', textContent: '', hidden: false, disabled: false, style: {},
    classList: {add() {}}, attributes: {},
    set innerHTML(value) { this.children = []; },
    querySelector() { return this.option || (this.option = element('option')); },
    setAttribute(name, value) { this.attributes[name] = value; },
    addEventListener(event, fn) { this[event] = fn; },
    appendChild(value) { this.children.push(value); return value; },
    append(...values) { this.children.push(...values); },
    replaceChildren(...values) { this.children = values; }};
}
function allText(node) {
  return typeof node === 'string' ? node : [node.textContent, ...node.children.map(allText)].join(' ');
}
module.exports = async function testModelPreflight() {
  const nodes = new Map(), requests = [];
  const document = {getElementById(id) {
    if (!nodes.has(id)) nodes.set(id, element());
    return nodes.get(id);
  }, createElement: element, createTextNode: text => text};
  const context = vm.createContext({document, console,
    window: {clearTimeout() {}, addEventListener() {}}, setTimeout() {}, setInterval() {},
    fetch: () => new Promise(() => {})});
  const evaluate = script => vm.runInContext(script, context);
  evaluate(fs.readFileSync(path.join(__dirname, '../../features/stress/static/stress.js'), 'utf8'));
  evaluate(`config={enabled:true,benchmarks:[{name:'npu_burn',enabled:true,
    model_capabilities:{allowed_models:['gpt3','llama2'],allow_reference_generation:true},
    profile:{configuration_sha256:'hash1',parameters:[
      {key:'devices',value:'0'},{key:'available_devices',value:'0,1'},
      {key:'allowed_devices',value:'0,1'},{key:'selected_cases',value:'matmul'},
      {key:'case_catalog',value:JSON.stringify([{name:'matmul',selectable:true},
        {name:'quant_matmul',selectable:false,reason:'no upstream A2 QuantMatmul strategy'},
        {name:'gpt3',selectable:false},{name:'llama2',selectable:false}])}]}}]};
    selected=new Set(['npu_burn']); renderNPUSelection(); byId('npu-mode-model').onclick();`);
  assert.equal(nodes.get('npu-unavailable-items').children.length, 1);
  assert.doesNotMatch(allText(nodes.get('npu-unavailable-items')), /gpt3|llama2/);
  assert.equal(nodes.get('npu-model-choices').children.length, 2);
  assert.equal(nodes.get('npu-model-preflight').disabled, true);
  context.fetch = async (url, options) => {
    requests.push({url, ...options});
    const request = JSON.parse(options.body);
    return {ok:true, json:async()=>({...request,status:'ready',failures:[]})};
  };
  await evaluate('checkModelPreflight()');
  assert.equal(requests.length, 0, 'empty reference cannot trigger probing or generation');
  evaluate(`byId('npu-reference-id').value='ref1'; byId('npu-reference-id').input();`);
  await evaluate('checkModelPreflight()');
  assert.equal(requests[0].url, '/api/stress/models/preflight');
  assert.equal(requests[0].headers['X-CATMonitor-Action'], 'stress');
  assert.deepEqual(JSON.parse(requests[0].body), {npu_devices:[0],npu_model:{name:'gpt3',action:'compare',reference_id:'ref1'}});
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /参考匹配、预检通过.*不代表训练通过/);
  assert.equal(nodes.get('npu-model-preflight-failures').hidden, true);
  assert.equal(nodes.get('npu-model-preflight-items').children.length, 0, 'passed checks do not clutter the list');

  for (const failure of [
    {code:'reference_missing',label:'参考未准备',message:'reference has not been generated'},
    {code:'reference_mismatch',label:'参考与当前参数不匹配',message:'configuration changed'},
    {code:'model_memory_preflight',label:'基础显存检查',message:'HBM insufficient'},
    {code:'dependency_unavailable',label:'模型依赖未就绪',message:'transformers unavailable'}]) {
    context.fetch = async (url, options) => {
      requests.push({url, ...options});
      return {ok:true,json:async()=>({...JSON.parse(options.body),status:'failed',failures:[failure]})};
    };
    await evaluate('checkModelPreflight()');
    assert.equal(nodes.get('npu-model-preflight-items').children.length, 1);
    assert.match(allText(nodes.get('npu-model-preflight-items')), new RegExp(failure.label));
    assert.doesNotMatch(allText(nodes.get('npu-model-preflight-items')), /llama2|不支持/);
  }
  evaluate(`byId('npu-model-choices').children[1].onclick();`);
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /尚未预检/);
  assert.equal(nodes.get('npu-model-preflight-failures').hidden, true, 'GPT3 failure must not become a Llama2 failure');
  let release;
  context.fetch = (url, options) => {
    requests.push({url, ...options});
    const request = JSON.parse(options.body);
    return new Promise(resolve => { release = () => resolve({ok:true,json:async()=>({...request,status:'ready',failures:[]})}); });
  };
  const inFlight = evaluate('checkModelPreflight()');
  const count = requests.length;
  await evaluate('checkModelPreflight()');
  assert.equal(requests.length, count, 'readonly probes cannot overlap');
  evaluate(`byId('npu-reference-id').value='ref2'; byId('npu-reference-id').input();`);
  release(); await inFlight;
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /尚未预检/);
  assert.equal(nodes.get('npu-model-preflight-failures').hidden, true, 'stale asynchronous replies cannot restore a verdict');

  context.fetch = async (url, options) => ({ok:true,json:async()=>({...JSON.parse(options.body),status:'ready',failures:[]})});
  await evaluate('checkModelPreflight()');
  evaluate(`config.benchmarks[0].profile.configuration_sha256='hash2'; renderNPUSelection();`);
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /尚未预检/);
  await evaluate('checkModelPreflight()');
  evaluate('modelPreflightResult.checkedAt -= 60001; renderModelPreflight();');
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /尚未预检/, 'old reference/capacity proof is not permanent readiness');
  await evaluate('checkModelPreflight()');
  evaluate(`selectedNPUDevices=new Set([1]); renderNPUSelection();`);
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /尚未预检/);
  evaluate(`report={status:'running'}; renderNPUSelection();`);
  context.fetch = async () => { throw new Error('must not probe an active job'); };
  await evaluate('checkModelPreflight()');
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /当前作业运行中/);
  assert.equal(nodes.get('npu-model-preflight-failures').hidden, true);
  evaluate('report=null; renderNPUSelection();');
  context.fetch = async () => { throw new TypeError('Failed to fetch'); };
  await evaluate('checkModelPreflight()');
  assert.match(nodes.get('npu-model-preflight-summary').textContent, /未执行/);
  assert.match(allText(nodes.get('npu-model-preflight-items')), /不能认定模型不支持/);

  context.fetch = async (url, options) => ({ok:true,json:async()=>({...JSON.parse(options.body),
    status:'failed',failures:[{code:'reference_missing',label:'<img src=x>',message:'<script>bad</script>'}]})});
  await evaluate('checkModelPreflight()');
  assert.equal(nodes.get('npu-model-preflight-items').children[0].children[0].tagName, 'b');
  assert.match(allText(nodes.get('npu-model-preflight-items')), /<script>/, 'diagnostics use textContent, never HTML');
  context.fetch = async (url, options) => ({ok:true,json:async()=>({...JSON.parse(options.body),npu_devices:[0],status:'ready',failures:[]})});
  await evaluate('checkModelPreflight()');
  assert.match(allText(nodes.get('npu-model-preflight-items')), /响应与本次选择不一致/);

  evaluate(`config.benchmarks[0].profile=null; renderNPUSelection();`);
  assert.equal(nodes.get('npu-mode-model').disabled, false, 'admin capability does not disappear with a failed Describe');
  assert.equal(nodes.get('npu-model-choices').children.length, 2);
  evaluate(`config.benchmarks[0].model_capabilities.allowed_models=[]; renderNPUSelection();`);
  assert.equal(nodes.get('npu-mode-model').disabled, true, 'revoked authorization is not inferred from old model results');
  assert(requests.every(r => r.url === '/api/stress/models/preflight'), 'preflight must never call Run/Cancel');
  console.log('PASS: dynamic model preflight, only failures, stale responses, capabilities and readonly request contract');
};
if (require.main === module) module.exports().catch(error => { console.error(error); process.exitCode = 1; });

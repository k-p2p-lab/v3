// Run with: node --test internal/webui/topology_test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'static/app.js'), 'utf8');
const layoutSource = fs.readFileSync(path.join(__dirname, 'static/topology-layout.js'), 'utf8');
const functions = source.slice(source.indexOf('function escapeHTML('), source.indexOf('$("#scenarioText").value = defaultScenario;'));
function context(extra = {}) {
  const sandbox = { Intl, Date, ...extra };
  vm.createContext(sandbox);
  vm.runInContext(layoutSource, sandbox);
  vm.runInContext(functions, sandbox);
  return sandbox;
}
const plain = (value) => JSON.parse(JSON.stringify(value));
const peer = (id, agentId = 'a', state = 'ready') => ({ id, agentId, state });
const resultSizeHeaders = (bytes, maxAgeMs) => ({
  get(name) {
    if (name.toLowerCase() === 'content-length') return String(bytes);
    if (name.toLowerCase() === 'x-kpl-result-size-max-age-ms') return maxAgeMs === undefined ? null : String(maxAgeMs);
    return null;
  },
});
async function waitUntil(predicate, message) {
  for (let attempt = 0; attempt < 30; attempt++) {
    if (predicate()) return;
    await new Promise((resolve) => setImmediate(resolve));
  }
  assert.fail(message);
}

test('delivery display distinguishes conditional reach, missing observations, pending and legacy', () => {
  const api = context();
  const metrics = {definition:'session-window-v1',deliveryRatioAvailable:true,reachability:0.7,deliveryRatioUpperBound:0.9,unknownDeliveries:2,expectedDeliveries:7,eligibleDeliveries:5,pendingPublications:3,finalizedPublications:8,initialDeliveryRatioAvailable:true,initialDeliveryRatio:0.6,initialDeliveryRatioUpperBound:0.8,initialUnknownDeliveries:2,initialExpectedDeliveries:10,stableCoverageAvailable:true,stableCoverage:0.7,stableCoverageUpperBound:0.8,departedPairs:2,continuityUnknownPairs:1,publicationAvailabilityUnknownPairs:3,availabilityUnknownPairs:4};
  let view = api.deliveryMetricView(metrics);
  assert.equal(view.primary, '70%–90%');
  assert.equal(view.initial, '60%–80%');
  assert.equal(view.coverage, '70%–80%');
  assert.match(view.progress, /Pending: 3/);
  assert.match(view.note, /conditional on the known starting cohort/);
  assert.match(view.observation, /2 receipt unknown · 1 continuity unknown · 3 start unknown/);
  view = api.deliveryMetricView({...metrics, deliveryRatioAvailable:false});
  assert.equal(view.primary, 'N/A');
  view = api.deliveryMetricView({...metrics, initialDeliveryRatioAvailable:false, stableCoverageAvailable:false});
  assert.equal(view.initial, 'N/A');
  assert.equal(view.coverage, 'N/A');
  view = api.deliveryMetricView({definition:'dispatch-cohort-v1',deliveryRatioAvailable:true,reachability:0.6});
  assert.equal(view.primary,'60%');
  assert.equal(view.windowed,false);
  assert.match(view.label,/Legacy/);
});

test('saved result download sizes distinguish loading, unavailable, live, and unreadable states', () => {
  const state = {resultSizeUnavailable:new Set()};
  const api = context({state});
  assert.equal(api.formatBytes(999), '999 B');
  assert.equal(api.formatBytes(1536), '1.5 KiB');
  assert.equal(api.formatBytes(5 * 1024 * 1024), '5 MiB');
  assert.equal(api.formatBytes(undefined), '');
  assert.equal(api.formatBytes('1536'), '');
  assert.match(api.resultDownloadSize({id:'loading',state:'completed'}), />Calculating ZIP size…<\/span>/);
  state.resultSizeUnavailable.add('failed');
  assert.match(api.resultDownloadSize({id:'failed',state:'completed'}), />Size unavailable<\/span>/);
  assert.match(api.resultDownloadSize({id:'sized',state:'completed',downloadBytes:1536}), />ZIP · 1.5 KiB<\/span>/);
  assert.match(api.resultDownloadSize({id:'sized',state:'completed',downloadBytes:1536}), /Size measured at last refresh; later events may change it\./);
  assert.match(api.resultDownloadSize({id:'expired',state:'completed',downloadBytes:1536,downloadSizeExpiresAtMs:1}), />Calculating ZIP size…<\/span>/);
  assert.match(api.resultDownloadSize({id:'live',state:'running',downloadBytes:1536}), />Live ZIP · size determined at download<\/span>/);
  assert.equal(api.resultDownloadSize({id:'broken',state:'unreadable'}), '');
});

test('download-size headers use local max-age timing and allow stable sizes', () => {
  const api = context();
  const now = Date.parse('2042-04-03T02:01:00Z');
  assert.deepEqual(plain(api.parseResultDownloadSizeHeaders(resultSizeHeaders(1536), now)), {downloadBytes:1536});
  assert.deepEqual(plain(api.parseResultDownloadSizeHeaders(resultSizeHeaders(1536, 60000), now)), {
    downloadBytes:1536,downloadSizeExpiresAtMs:now + 60000,
  });
  assert.deepEqual(plain(api.parseResultDownloadSizeHeaders(resultSizeHeaders(1536, 1), now)), {
    downloadBytes:1536,downloadSizeExpiresAtMs:now + 1,
  });
  assert.throws(() => api.parseResultDownloadSizeHeaders(resultSizeHeaders('12.5', 60000), now), /Content-Length/);
  assert.throws(() => api.parseResultDownloadSizeHeaders(resultSizeHeaders(0, 60000), now), /supported range/);
  assert.throws(() => api.parseResultDownloadSizeHeaders(resultSizeHeaders(1536, 'invalid'), now), /positive max-age/);
  assert.throws(() => api.parseResultDownloadSizeHeaders(resultSizeHeaders(1536, 0), now), /positive max-age/);
});

test('saved-list max-age is materialized once from the receiving client clock', () => {
  const api = context();
  const now = Date.parse('2042-04-03T02:01:00Z');
  const run = {id:'timed',state:'completed',downloadBytes:1536,downloadSizeMaxAgeMs:250};
  assert.equal(api.materializeResultDownloadSizeExpiry(run, now), true);
  assert.equal(run.downloadSizeExpiresAtMs, now + 250);
  assert.equal(Object.hasOwn(run, 'downloadSizeMaxAgeMs'), false);
  api.materializeResultDownloadSizeExpiry(run, now + 100);
  assert.equal(run.downloadSizeExpiresAtMs, now + 250, 're-rendering extended a relative lifetime');
  const invalid = {id:'invalid',state:'completed',downloadBytes:1536,downloadSizeMaxAgeMs:'250'};
  assert.equal(api.materializeResultDownloadSizeExpiry(invalid, now), false);
  assert.equal(invalid.downloadBytes, undefined);
});

test('experiment cards use matching saved sizes only for terminal runs', () => {
  const state = {savedResults:[{id:'done',downloadBytes:1536},{id:'active',downloadBytes:2048},{id:'unknown'}],resultSizeUnavailable:new Set()};
  const api = context({state});
  assert.match(api.runDownloadSize({id:'done',state:'completed'}), />ZIP · 1.5 KiB<\/span>/);
  assert.match(api.runDownloadSize({id:'active',state:'running'}), />Live ZIP · size determined at download<\/span>/);
  assert.match(api.runDownloadSize({id:'unknown',state:'completed'}), />Calculating ZIP size…<\/span>/);
  assert.equal(api.runDownloadSize({id:'missing',state:'completed'}), '');
});

test('saved-results refresh redraws experiment cards with newly loaded sizes', async () => {
  const elements = new Map();
  const element = (id) => {
    if (!elements.has(id)) elements.set(id, {
      classList:{toggle(){}},
      setAttribute(){},
      textContent:'',innerHTML:'',hidden:false,disabled:false,
    });
    return elements.get(id);
  };
  const run = {id:'done',name:'Completed run',state:'completed',phase:1,totalPhases:1,seed:7};
  const state = {
    savedResults:null,resultsLoading:false,resultsError:'',resultsRefreshTimer:null,resultsRefreshPending:false,
    deletedResultIDs:new Set(),snapshot:{experiments:[run]},runStates:null,pendingStops:new Set(),deletingResultId:null,
    resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),
  };
  const api = context({state,$:(selector)=>element(selector),AbortController,setTimeout,clearTimeout});
  api.api = async () => [{...run,downloadBytes:1536}];
  await api.refreshSavedResults();
  assert.match(element('#runList').innerHTML, />ZIP · 1.5 KiB<\/span>/);
});

test('download-size HEAD queue deduplicates IDs and limits global concurrency to two', async () => {
  const runs = ['one','two','three'].map((id) => ({id,state:'completed'}));
  const state = {savedResults:runs,resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),snapshot:null};
  const calls = [], pending = [];
  let active = 0, maximumActive = 0;
  const fetch = (url, options) => new Promise((resolve) => {
    active++;
    maximumActive = Math.max(maximumActive, active);
    calls.push({url,options});
    pending.push((bytes) => {
      active--;
      resolve({ok:true,status:200,headers:resultSizeHeaders(bytes, 60000)});
    });
  });
  const api = context({state,fetch,AbortController,setTimeout,clearTimeout});
  api.renderResultSizeViews = () => {};
  api.queueResultDownloadSizes(runs);
  api.queueResultDownloadSizes(runs);
  assert.equal(calls.length, 2);
  assert.equal(state.resultSizeInflight.size, 3);
  assert.equal(maximumActive, 2);
  pending[0](1024);
  await waitUntil(() => calls.length === 3, 'third HEAD request did not start after a slot was released');
  assert.equal(maximumActive, 2);
  pending[1](2048);
  pending[2](3072);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'download-size queue did not drain');
  assert.deepEqual(runs.map((run) => run.downloadBytes), [1024,2048,3072]);
  assert.ok(calls.every(({options}) => options.method === 'HEAD' && options.cache === 'no-store' && options.signal));
  assert.deepEqual(calls.map(({url}) => url), [
    '/api/v1/experiments/one/download','/api/v1/experiments/two/download','/api/v1/experiments/three/download',
  ]);
  clearTimeout(state.resultSizeExpiryTimer);
});

test('stale HEAD completion cannot write into a refreshed result object', async () => {
  const requested = {id:'race',state:'completed'};
  const state = {savedResults:[requested],resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),snapshot:null};
  const pending = [];
  const fetch = () => new Promise((resolve) => pending.push((bytes) => resolve({
    ok:true,status:200,headers:resultSizeHeaders(bytes, 60000),
  })));
  const api = context({state,fetch,AbortController,setTimeout,clearTimeout});
  api.renderResultSizeViews = () => {};
  api.queueResultDownloadSizes(state.savedResults);
  assert.equal(pending.length, 1);
  const refreshed = {id:'race',state:'completed'};
  state.savedResults = [refreshed];
  api.queueResultDownloadSizes(state.savedResults);
  assert.equal(pending.length, 1, 'overlapping refresh duplicated the in-flight ID');
  pending[0](1024);
  await waitUntil(() => pending.length === 2, 'stale completion did not requeue the refreshed object');
  assert.equal(requested.downloadBytes, undefined);
  assert.equal(refreshed.downloadBytes, undefined, 'stale bytes leaked into the refreshed result');
  assert.equal(state.resultSizeUnavailable.has('race'), false);
  pending[1](2048);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'replacement size request did not settle');
  assert.equal(refreshed.downloadBytes, 2048);
  assert.equal(pending.length, 2, 'current completion unexpectedly retried itself');
  clearTimeout(state.resultSizeExpiryTimer);
});

test('near expiry follows extended list grace then remeasures through the bounded queue', async () => {
  let now = Date.parse('2026-09-05T03:00:00Z');
  class FakeDate extends Date { static now() { return now; } }
  const timers = new Map();
  let nextTimer = 1;
  const setTimeout = (callback, delay) => { const id = nextTimer++; timers.set(id,{callback,delay}); return id; };
  const clearTimeout = (id) => timers.delete(id);
  const run = {id:'expiring',state:'completed',downloadBytes:1024,downloadSizeMaxAgeMs:1};
  const state = {
    savedResults:[run],resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),snapshot:null,
    resultSizeExpiryTimer:null,resultSizeExpiryAt:0,
  };
  const calls = [];
  const fetch = async (url, options) => {
    calls.push({url,options});
    return {ok:true,status:200,headers:resultSizeHeaders(2048, 1000)};
  };
  const api = context({state,fetch,AbortController,Date:FakeDate,setTimeout,clearTimeout});
  api.renderResultSizeViews = () => {};
  api.queueResultDownloadSizes(state.savedResults);
  assert.equal(calls.length, 0);
  assert.equal(run.downloadSizeExpiresAtMs, now + 1);
  assert.deepEqual([...timers.values()].map(({delay}) => delay), [1]);
  const firstTimer = state.resultSizeExpiryTimer;
  api.queueResultDownloadSizes(state.savedResults);
  assert.equal(state.resultSizeExpiryTimer, firstTimer, 'unchanged expiry created a duplicate timer');
  assert.equal(timers.size, 1);
  const extendedRun = {id:'expiring',state:'completed',downloadBytes:1024,downloadSizeMaxAgeMs:500};
  state.savedResults = [extendedRun];
  api.queueResultDownloadSizes(state.savedResults);
  assert.deepEqual([...timers.values()].map(({delay}) => delay), [500], 'later list grace did not replace the earlier timer');
  now += 500;
  const expiryTimer = timers.get(state.resultSizeExpiryTimer);
  timers.delete(state.resultSizeExpiryTimer);
  expiryTimer.callback();
  assert.equal(extendedRun.downloadBytes, undefined, 'expired bytes remained renderable');
  assert.equal(calls.length, 1);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'expired size was not remeasured');
  assert.equal(extendedRun.downloadBytes, 2048);
  assert.equal(extendedRun.downloadSizeExpiresAtMs, now + 1000);
  assert.deepEqual([...timers.values()].map(({delay}) => delay), [1000]);
});

test('download-size HEAD failures are per-ID and skip active or unreadable results', async () => {
  const runs = [
    {id:'bad-header',state:'completed'}, {id:'request-error',state:'completed'},
    {id:'active',state:'running'}, {id:'queued',state:'queued'}, {id:'broken',state:'unreadable'},
  ];
  const state = {savedResults:runs,resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),snapshot:null};
  const calls = [], timerDelays = [];
  const fetch = async (url, options) => {
    calls.push({url,options});
    if (url.includes('request-error')) return {ok:false,status:503,headers:{get:()=>null}};
    return {ok:true,status:200,headers:resultSizeHeaders(1024, 'invalid')};
  };
  const api = context({
    state,fetch,AbortController,
    setTimeout:(callback,delay)=>{timerDelays.push(delay);return callback;},clearTimeout:()=>{},
  });
  api.renderResultSizeViews = () => {};
  api.queueResultDownloadSizes(runs);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'failed download-size requests did not settle');
  assert.deepEqual(calls.map(({url}) => url), [
    '/api/v1/experiments/bad-header/download','/api/v1/experiments/request-error/download',
  ]);
  assert.deepEqual(timerDelays, [30000,30000]);
  assert.deepEqual([...state.resultSizeUnavailable].sort(), ['bad-header','request-error']);
  assert.equal(state.resultSizeQueue.length, 0);
  assert.equal(state.resultSizeActive, 0);
  assert.match(api.resultDownloadSize(runs[0]), />Size unavailable<\/span>/);
  assert.match(api.resultDownloadSize(runs[2]), />Live ZIP · size determined at download<\/span>/);
  assert.equal(api.resultDownloadSize(runs[4]), '');
});

test('invalid HEAD max-age follows the normal failure policy and retries on refresh', async () => {
  const run = {id:'retry',state:'completed'};
  const state = {savedResults:[run],resultSizeInflight:new Set(),resultSizeQueue:[],resultSizeActive:0,resultSizeUnavailable:new Set(),snapshot:null};
  let calls = 0;
  const fetch = async () => {
    calls++;
    return {ok:true,status:200,headers:calls === 1 ? resultSizeHeaders(2048, 0) : resultSizeHeaders(2048)};
  };
  const api = context({state,fetch,AbortController,setTimeout,clearTimeout});
  api.renderResultSizeViews = () => {};
  api.queueResultDownloadSizes(state.savedResults);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'invalid max-age request did not settle');
  assert.equal(calls, 1, 'invalid max-age retried without a refresh');
  assert.equal(state.resultSizeUnavailable.has(run.id), true);
  api.queueResultDownloadSizes(state.savedResults);
  await waitUntil(() => state.resultSizeInflight.size === 0, 'explicit refresh did not retry the size request');
  assert.equal(calls, 2);
  assert.equal(run.downloadBytes, 2048);
  assert.equal(run.downloadSizeExpiresAtMs, undefined);
  assert.equal(state.resultSizeUnavailable.has(run.id), false);
});

test('relationship layers do not misclassify legacy transport and preserve the input history', () => {
  const api = context();
  const nodes = [peer('one'), peer('two'), peer('stopped', 'a', 'stopped'), peer('stopping', 'a', 'stopping'), peer('failed', 'a', 'failed')];
  const edges = [
    {source:'one',target:'two'},
    {source:'one',target:'two',protocol:'unknown'},
    {source:'one',target:'two',protocol:'kademlia',reportedBy:['one']},
    {source:'one',target:'stopped',protocol:'kademlia'},
    {source:'one',target:'stopping',protocol:'kademlia'},
    {source:'one',target:'missing',protocol:'kademlia'},
    {source:'one',target:'one',protocol:'kademlia'},
  ];
  const original = JSON.stringify({nodes,edges});
  const routing = api.filterTopologyEdges(nodes, edges, {kademlia:true});
  assert.deepEqual(plain(routing), [{source:'one',target:'two',protocol:'kademlia',topics:[],reportedBy:['one'],topicReports:{}}]);
  const transport = api.filterTopologyEdges(nodes, edges, {transport:true});
  assert.equal(transport.length,1);
  assert.equal(transport[0].protocol,'transport');
  assert.deepEqual(plain(api.topologyData(nodes,edges).nodes.map(n=>n.id)),['one','two','failed']);
  assert.equal(JSON.stringify({nodes,edges}),original);
});

test('GossipSub merges endpoint pairs and topics but keeps Kademlia and reporter direction distinct', () => {
  const api = context(), nodes = [peer('one'),peer('two')];
  const edges = [
    {source:'one',target:'two',protocol:'gossipsub',topic:'z',reportedBy:['one']},
    {source:'two',target:'one',protocol:'gossipsub',topic:'a',reportedBy:['two']},
    {source:'one',target:'two',protocol:'gossipsub',topic:'z',reportedBy:['one']},
    {source:'one',target:'two',protocol:'kademlia',reportedBy:['one']},
  ];
  const both = api.filterTopologyEdges(nodes,edges,{gossipsub:true,kademlia:true});
  assert.equal(both.length,2);
  assert.deepEqual(plain(both[0].topics),['a','z']);
  assert.deepEqual(plain(both[0].reportedBy),['one','two']);
  assert.deepEqual(plain(both[0].topicReports),{z:['one'],a:['two']});
  assert.equal(api.topologyReportSummary(both[0],'one'),'a: Remote Peer reported; z: Selected Peer reported');
  assert.ok(!api.topologyReportSummary(both[0],'one').includes('Both endpoints'));
  const filtered = api.filterTopologyEdges(nodes,edges,{gossipsub:true,kademlia:true,topic:'a'});
  assert.equal(filtered.length,2);
  assert.deepEqual(plain(filtered[0].topics),['a']);
  assert.deepEqual(plain(filtered[0].reportedBy),['two']);
  assert.equal(filtered[1].protocol,'kademlia');
  assert.equal(api.filterTopologyEdges(nodes,edges,{topic:'a'}).length,0);
});

test('topic choices include empty observed meshes and exclude unrelated transport topics', () => {
  const api=context();
  assert.deepEqual(plain(api.observedTopologyTopics([{meshPeers:{empty:[],alpha:['peer']}}],[
    {protocol:'gossipsub',topic:'beta'}, {protocol:'gossipsub',topic:'alpha'}, {topic:'transport-only'},
  ])),['alpha','beta','empty']);
});

test('fit camera includes every Agent and zoom preserves the chosen world anchor', () => {
  const topology={camera:{x:30,y:40,scale:0.5},graph:{width:1000,height:600},autoFit:true};
  const elements=new Map();
  const api=context({state:{topology},$:(id)=>{if(!elements.has(id))elements.set(id,{setAttribute(){}});return elements.get(id);}});
  const bounds={width:2400,height:1800};
  const fit=api.fitTopologyCamera(bounds,1000,600);
  assert.ok(fit.x>=24 && fit.y>=24);
  assert.ok(fit.x+bounds.width*fit.scale<=976.01);
  assert.ok(fit.y+bounds.height*fit.scale<=576.01);
  const point={x:200,y:150}, world={x:(point.x-30)/0.5,y:(point.y-40)/0.5};
  api.zoomTopology(1.25,point);
  assert.equal(topology.autoFit,false);
  assert.equal((point.x-topology.camera.x)/topology.camera.scale,world.x);
  assert.equal((point.y-topology.camera.y)/topology.camera.scale,world.y);
});

test('inspector escapes node/topic text and handles missing mesh values', () => {
  const first={...peer('<node>'),peerId:'<peer>',meshPeers:{empty:null},peerScores:{a:-1.25,b:2.75},metadata:{network:'null',pubsubRouter:'gossipsub',topicMode:'subscribe',dhtMode:'client',topics:'topic/a, <topic/b>',runtime:'docker',containerId:'container<1234>'}};
  const other=peer('other','b');
  const graph={nodes:[first,other],positions:new Map([['<node>',{slot:1}],['other',{slot:2}]]),edges:[{source:'<node>',target:'other',protocol:'gossipsub',topics:['<topic>'],reportedBy:['<node>'],topicReports:{'<topic>':['<node>']}}]};
  const output={};
  const api=context({state:{topology:{graph},agentNumbers:new Map([['a',1],['b',2]])},$:()=>output});
  api.renderTopologyDetails(first);
  assert.ok(output.innerHTML.includes('&lt;node&gt;'));
  assert.ok(output.innerHTML.includes('&lt;topic&gt;'));
  assert.ok(!output.innerHTML.includes('<node>'));
  assert.ok(output.innerHTML.includes('Selected Peer reported'));
  assert.ok(output.innerHTML.includes('2 scores · Average: 0.75'));
  assert.ok(output.innerHTML.includes('gossipsub / subscribe'));
  assert.ok(output.innerHTML.includes('<dt>DHT mode</dt><dd>client</dd>'));
  assert.ok(output.innerHTML.includes('topic/a, &lt;topic/b&gt;'));
  assert.ok(output.innerHTML.includes('<dt>Runtime</dt><dd>docker</dd>'));
  assert.ok(output.innerHTML.includes('container&lt;1234&gt;'));
  first.peerScores={};first.metadata={pubsubEnabled:'false',dhtEnabled:'false'};
  api.renderTopologyDetails(first);
  assert.ok(output.innerHTML.includes('0 scores · Average: N/A'));
  assert.ok(output.innerHTML.includes('<dt>PubSub router / topic mode</dt><dd>Off</dd>'));
  assert.ok(output.innerHTML.includes('<dt>DHT mode</dt><dd>Off</dd>'));
});

test('recent events separate experiment IDs and shorten matching node prefixes', () => {
  const eventList = {};
  const api = context({$: (selector) => {
    assert.equal(selector, '#eventList');
    return eventList;
  }});
  api.renderEvents([{
    timestamp: '2026-09-05T03:04:05Z',
    type: 'measurement_checkpoint_with_a_long_name',
    runId: 'run-20260905T030000Z-a1b2c3d4',
    nodeId: 'run-20260905T030000Z-a1b2c3d4-workers-00001',
    remotePeerId: '<remote-peer-id>',
    latencyMs: 12.25,
  }]);
  assert.match(eventList.innerHTML, /class="event-type"[^>]*>measurement_checkpoint_with_a_long_name<\/span>/);
  assert.match(eventList.innerHTML, /class="event-summary"[^>]*>workers-00001 · ← &lt;remote-pe · 12\.3 ms<\/span>/);
  assert.match(eventList.innerHTML, /class="event-run-id"[^>]*>Experiment · run-20260905T030000Z-a1b2c3d4<\/small>/);
  assert.equal((eventList.innerHTML.match(/run-20260905T030000Z-a1b2c3d4/g) || []).length, 2, 'run ID should appear only in the small label and its title');
  assert.ok(!eventList.innerHTML.includes('<remote-peer-id>'));
});

test('recent events preserve node IDs when no matching experiment prefix exists', () => {
  const eventList = {};
  const api = context({$: () => eventList});
  api.renderEvents([{timestamp:'2026-09-05T03:04:05Z',type:'publish',runId:'run-a',nodeId:'standalone-node'}]);
  assert.match(eventList.innerHTML, />standalone-node<\/span>/);
  api.renderEvents([{timestamp:'2026-09-05T03:04:05Z',type:'publish',nodeId:'run-a-workers-00001'}]);
  assert.match(eventList.innerHTML, />run-a-workers-00001<\/span>/);
  assert.ok(!eventList.innerHTML.includes('event-run-id'));
});

function uiFixture({reducedMotion=false}={}) {
  const ids=new Map();
  const frames={queue:new Map(),nextID:1,maximumQueued:0};
  frames.request=(callback)=>{const id=frames.nextID++;frames.queue.set(id,callback);frames.maximumQueued=Math.max(frames.maximumQueued,frames.queue.size);return id;};
  frames.cancel=(id)=>frames.queue.delete(id);
  frames.step=(timestamp)=>{const entry=frames.queue.entries().next().value;if(!entry)return false;frames.queue.delete(entry[0]);entry[1](timestamp);return true;};
  const eventTarget=()=>{
    const events=new Map();
    return {addEventListener(name,fn){if(!events.has(name))events.set(name,[]);events.get(name).push(fn);},emit(name,extra={}){for(const fn of events.get(name)||[])fn({type:name,...extra});}};
  };
  const preference={...eventTarget(),matches:reducedMotion};
  let document;
  class Element {
    constructor(tag='div') {
      this.tag=tag;this.children=[];this.dataset={};this.attributes={};this.events={};this.clientWidth=1280;this.clientHeight=600;
      const classes=new Set();
      this.classList={add:(name)=>classes.add(name),remove:(name)=>classes.delete(name),contains:(name)=>classes.has(name),toggle:(name,on)=>on?classes.add(name):classes.delete(name)};
    }
    setAttribute(key,value) {
      this.attributes[key]=String(value);
      if(key==='id')ids.set(String(value),this);
      if(key==='class')for(const name of String(value).split(' '))this.classList.add(name);
      if(key.startsWith('data-'))this.dataset[key.slice(5).replace(/-([a-z])/g,(_,letter)=>letter.toUpperCase())]=String(value);
    }
    getAttribute(key){return this.attributes[key]??null;}
    append(...children){for(const child of children){child.parent=this;this.children.push(child);}}
    replaceChildren(...children){this.children=[];this.append(...children);}
    get options(){return this.children;}
    addEventListener(name,fn){this.events[name]=fn;}
    emit(name,extra={}){return this.events[name]?.({target:this,button:0,...extra});}
    matches(selector){return selector==='[data-node-id]'?Boolean(this.dataset.nodeId):selector.startsWith('.')&&this.classList.contains(selector.slice(1));}
    closest(selector){return this.matches(selector)?this:this.parent?.closest(selector);}
    querySelectorAll(selector){return this.children.flatMap(child=>[...(child.matches(selector)?[child]:[]),...child.querySelectorAll(selector)]);}
    setPointerCapture(){}
    getBoundingClientRect(){return {left:0,top:0};}
    focus(){document.activeElement=this;}
  }
  const html=fs.readFileSync(path.join(__dirname,'static/index.html'),'utf8');
  for(const match of html.matchAll(/<([a-z]+)[^>]*\bid="([^"]+)"[^>]*>/g)){
    const element=new Element(match[1]);element.setAttribute('id',match[2]);element.checked=/\bchecked\b/.test(match[0]);
  }
  document={...eventTarget(),activeElement:null,hidden:false,get visibilityState(){return this.hidden?'hidden':'visible';},querySelector:(selector)=>ids.get(selector.slice(1)),querySelectorAll:(selector)=>ids.get('topology').querySelectorAll(selector),createElement:(tag)=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
  const window={...eventTarget(),matchMedia:()=>preference};
  const sandbox={document,window,Intl,Date,requestAnimationFrame:frames.request,cancelAnimationFrame:frames.cancel,localStorage:{getItem:()=>null,setItem(){}}};
  vm.createContext(sandbox);
  vm.runInContext(layoutSource,sandbox);
  vm.runInContext(source.slice(0,source.indexOf('const defaultScenario ='))+functions,sandbox);
  const state=vm.runInContext('state',sandbox);
  const render=(nodes=[peer('one'),peer('two'),peer('three','b')],edges=[
    {source:'one',target:'two',protocol:'gossipsub',topic:'alpha'},
    {source:'two',target:'three',protocol:'kademlia'},
  ])=>{state.snapshot={nodes,edges,agents:[{id:'a'},{id:'b'}]};sandbox.renderTopology(nodes,edges);};
  return {ids,document,window,sandbox,state,frames,render,
    setHidden(value){document.hidden=value;document.emit('visibilitychange');},
    setReduced(value){preference.matches=value;preference.emit('change');},
  };
}

test('rendered graph controls preserve selection and camera while filtering and streaming churn', () => {
  const {ids,document,sandbox,state,frames}=uiFixture();
  const nodes=[{...peer('one'),peerScores:{a:1,b:3}},peer('two'),peer('three','b'),peer('stopped','a','stopped')];
  const edges=[
    {source:'one',target:'two',protocol:'kademlia',reportedBy:['one']},
    {source:'one',target:'two',protocol:'gossipsub',topic:'alpha',reportedBy:['one']},
    {source:'two',target:'three',protocol:'gossipsub',topic:'beta',reportedBy:['two']},
    {source:'one',target:'three'},
    {source:'one',target:'stopped',protocol:'gossipsub',topic:'alpha'},
  ];
  state.snapshot={nodes,edges,agents:[{id:'a',capacity:10000},{id:'b',capacity:10000}]};
  sandbox.setupTopologyControls();
  sandbox.renderTopology(nodes,edges);
  const svg=ids.get('topology');
  assert.equal(ids.get('topologyEmpty').hidden,true);
  assert.equal(svg.querySelectorAll('.topology-peer').length,3);
  assert.equal(svg.querySelectorAll('.topology-edge').length,3);
  assert.deepEqual(svg.querySelectorAll('.topology-label').map(label=>label.textContent),[1,2]);
  const first=svg.querySelectorAll('.topology-peer').find(node=>node.dataset.nodeId==='one');
  svg.emit('pointerover',{target:first});
  assert.equal(svg.querySelectorAll('.topology-edge').filter(edge=>edge.classList.contains('is-active')).length,2);
  assert.ok(ids.get('topologyDetails').innerHTML.includes('2 scores · Average: 2.00'));
  svg.emit('click',{target:first});
  svg.emit('pointerleave');
  assert.equal(state.topology.selected,'one');
  first.focus();
  ids.get('showgossipsub').checked=false;ids.get('showgossipsub').emit('change');
  assert.equal(svg.querySelectorAll('.topology-edge').length,1);
  assert.equal(document.activeElement.dataset.nodeId,'one');
  svg.emit('pointerdown',{pointerId:1,clientX:10,clientY:10});
  svg.emit('pointermove',{pointerId:1,clientX:40,clientY:60});
  svg.emit('pointerup');
  const camera=plain(state.topology.camera);
  sandbox.renderTopology([...nodes].reverse(),edges);
  frames.step(1000);
  assert.deepEqual(plain(state.topology.camera),camera);
  assert.equal(state.topology.selected,'one');
  assert.equal(state.topology.filters.gossipsub,false);
  assert.equal(document.activeElement.dataset.nodeId,'one');
  let prevented=false;
  svg.emit('wheel',{deltaY:20,ctrlKey:false,preventDefault(){prevented=true;}});
  assert.equal(prevented,false);
  svg.emit('wheel',{deltaY:20,ctrlKey:true,clientX:100,clientY:100,preventDefault(){prevented=true;}});
  assert.equal(prevented,true);
  assert.ok(state.topology.camera.scale<camera.scale);
  ids.get('topologyFit').emit('click');assert.equal(state.topology.autoFit,true);
  sandbox.renderTopology(nodes.map(node=>({...node,state:'stopped'})),edges);
  assert.equal(state.topology.selected,null);
  assert.equal(ids.get('topologyEmpty').hidden,false);
  assert.equal(svg.querySelectorAll('.topology-peer').length,0);
  assert.equal(frames.queue.size,0);
});

test('animation updates node coordinates and edge paths with at most one queued frame', () => {
  const {ids,sandbox,state,frames,render}=uiFixture();
  sandbox.setupTopologyControls();render();
  const svg=ids.get('topology');
  const positions=()=>svg.querySelectorAll('.topology-peer').map(element=>element.getAttribute('transform'));
  const paths=()=>svg.querySelectorAll('.topology-edge').map(element=>element.getAttribute('d'));
  const before=positions(),beforePaths=paths(),camera=plain(state.topology.camera);
  assert.equal(frames.queue.size,1);
  sandbox.startTopologyMotion();sandbox.startTopologyMotion();
  assert.equal(frames.queue.size,1);
  frames.step(1000);
  assert.notDeepEqual(positions(),before);
  assert.notDeepEqual(paths(),beforePaths);
  assert.deepEqual(plain(state.topology.camera),camera);
  for(let i=1;i<=5;i++)frames.step(1000+i*40);
  assert.equal(frames.maximumQueued,1);
  assert.equal(frames.queue.size,1);
});

test('pause and resume cancel and restart motion without losing the graph or camera', () => {
  const {ids,sandbox,state,frames,render}=uiFixture();
  sandbox.setupTopologyControls();render();frames.step(1000);
  const graph=state.topology.graph,camera=plain(state.topology.camera);
  const points=()=>plain([...graph.positions].map(([id,p])=>[id,p.x,p.y]));
  ids.get('topologyMotion').emit('click');
  const paused=points();
  assert.equal(state.topology.motion.enabled,false);
  assert.equal(ids.get('topologyMotion').textContent,'Resume motion');
  assert.equal(ids.get('topologyMotion').getAttribute('aria-pressed'),'true');
  assert.equal(frames.queue.size,0);
  assert.equal(frames.step(1040),false);
  sandbox.startTopologyMotion();assert.equal(frames.queue.size,0);
  assert.deepEqual(points(),paused);
  ids.get('topologyMotion').emit('click');
  assert.equal(ids.get('topologyMotion').textContent,'Pause motion');
  assert.equal(frames.queue.size,1);frames.step(1080);
  assert.notDeepEqual(points(),paused);
  assert.equal(state.topology.graph,graph);
  assert.deepEqual(plain(state.topology.camera),camera);
  assert.equal(frames.maximumQueued,1);
});

test('hidden tabs and page navigation stop queued work and visible tabs resume once', () => {
  const {sandbox,state,frames,render,setHidden,window}=uiFixture();
  sandbox.setupTopologyControls();render();frames.step(1000);
  setHidden(true);
  assert.equal(frames.queue.size,0);
  const snapshot=state.snapshot;
  render(snapshot.nodes,snapshot.edges);
  sandbox.startTopologyMotion();assert.equal(frames.queue.size,0);
  setHidden(false);setHidden(false);
  assert.equal(frames.queue.size,1);frames.step(2000);
  window.emit('pagehide');
  assert.equal(frames.queue.size,0);
  assert.equal(frames.maximumQueued,1);
});

test('stopped peers leave the force graph and no frames survive an empty snapshot', () => {
  const {ids,sandbox,state,frames,render,setHidden}=uiFixture();
  sandbox.setupTopologyControls();render();frames.step(1000);
  state.topology.selected='one';
  render(state.snapshot.nodes.map(node=>({...node,state:'stopped'})),state.snapshot.edges);
  assert.equal(state.topology.selected,null);
  assert.equal(state.topology.graph.positions.size,0);
  assert.equal(ids.get('topology').querySelectorAll('.topology-peer').length,0);
  assert.equal(ids.get('topology').querySelectorAll('.topology-edge').length,0);
  assert.equal(frames.queue.size,0);
  setHidden(true);setHidden(false);sandbox.startTopologyMotion();
  assert.equal(frames.queue.size,0);
  assert.equal(frames.maximumQueued,1);
});

test('identical streamed snapshots retain cooling and never reset settled engine alpha', () => {
  const {sandbox,state,frames,render}=uiFixture();
  sandbox.setupTopologyControls();render();frames.step(1000);
  const engine=state.topology.layout.pizza,alpha=engine.alpha,tick=engine.tick;
  const points=plain([...state.topology.graph.positions].map(([id,p])=>[id,p.x,p.y]));
  render([...state.snapshot.nodes].reverse(),[...state.snapshot.edges].reverse());
  assert.equal(state.topology.layout.pizza,engine);
  assert.equal(engine.alpha,alpha);assert.equal(engine.tick,tick);
  assert.deepEqual(plain([...state.topology.graph.positions].map(([id,p])=>[id,p.x,p.y])),points);
  frames.step(1040);assert.ok(engine.alpha<alpha);
  engine.alpha=0;engine.tick=600;
  render(state.snapshot.nodes.map(node=>({...node,lastSeen:'2026-09-05T00:00:00Z'})),state.snapshot.edges);
  assert.equal(engine.alpha,0);assert.equal(engine.tick,600);
  frames.step(1080);assert.equal(frames.queue.size,0);
  assert.equal(frames.maximumQueued,1);
});

test('reduced-motion preference defaults to still layout and preserves explicit user choice', () => {
  const {ids,sandbox,state,frames,render,setReduced}=uiFixture({reducedMotion:true});
  sandbox.setupTopologyControls();render();
  assert.equal(state.topology.motion.enabled,false);
  assert.equal(frames.queue.size,0);
  assert.equal(ids.get('topologyMotion').textContent,'Resume motion');
  setReduced(false);assert.equal(frames.queue.size,1);
  setReduced(true);assert.equal(frames.queue.size,0);
  ids.get('topologyMotion').emit('click');
  assert.equal(state.topology.motion.overridden,true);
  assert.equal(frames.queue.size,1);
  setReduced(false);setReduced(true);
  assert.equal(state.topology.motion.enabled,true);
  assert.equal(frames.queue.size,1);
  ids.get('topologyMotion').emit('click');setReduced(false);
  assert.equal(state.topology.motion.enabled,false);
  assert.equal(frames.queue.size,0);
});

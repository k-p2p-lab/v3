const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'static/app.js'), 'utf8');
const functions = source.slice(source.indexOf('function escapeHTML('), source.indexOf('$("#scenarioText").value = defaultScenario;'));
function fixture(results) {
  const elements = new Map(), timers = new Map();
  let nextTimer = 0, resolve, reject;
  const element = selector => {
    if (!elements.has(selector)) elements.set(selector, {
      textContent: '', html: '', writes: 0, hidden: false, disabled: false, attributes: {},
      get innerHTML() { return this.html; },
      set innerHTML(value) { this.html = value; this.writes++; },
      querySelectorAll() { return []; }, contains() { return false; },
      classList: { toggle() {} },
      setAttribute(name, value) { this.attributes[name] = value; },
    });
    return elements.get(selector);
  };
  const api = {
    document: { querySelector: element }, localStorage: { getItem: () => null },
    Intl, Date, AbortController,
    setTimeout: (fn, delay) => { const id = ++nextTimer; timers.set(id, { fn, delay }); return id; },
    clearTimeout: id => timers.delete(id),
  };
  vm.createContext(api);
  vm.runInContext(source.slice(0, source.indexOf('const defaultScenario =')) + functions, api);
  const state = vm.runInContext('state', api);
  state.savedResults = results;
  api.api = () => new Promise((yes, no) => { resolve = yes; reject = no; });
  api.renderSavedResults();
  return { api, state, element, timers, resolve: value => resolve(value), reject: error => reject(error) };
}

test('analysis polling preserves the visible list and refresh label while the request is pending', async () => {
  const run = { id: 'analyzing', state: 'completed', sourceBytes: 1024, analysis: { state: 'running', progress: 9 } };
  const { api, state, element, timers, resolve } = fixture([run]);
  const rows = element('#savedResultsRows'), writes = rows.writes;
  const refreshing = api.refreshSavedResults();
  assert.equal(state.resultsLoading, true);
  assert.equal(element('#savedResultsStatus').hidden, true);
  assert.equal(element('#savedResultsTable').hidden, false);
  assert.equal(element('#savedResultsTable').attributes['aria-busy'], 'true');
  assert.equal(element('#refreshResults').textContent, 'Refresh');
  assert.equal(rows.writes, writes);
  resolve([{ ...run }]);
  await refreshing;
  assert.equal(element('#savedResultsStatus').hidden, true);
  assert.equal(element('#savedResultsTable').attributes['aria-busy'], 'false');
  assert.equal(element('#refreshResults').textContent, 'Refresh');
  assert.equal(rows.writes, writes, 'an unchanged poll rebuilt the result rows');
  assert.equal([...timers.values()].filter(timer => timer.delay === 3000).length, 1);
  state.savedResults[0].analysis = { state: 'running', progress: 10 };
  api.renderSavedResults();
  assert.match(rows.innerHTML, /Images · 10% read/);
});

test('only initial loading shows loading text; refreshing an empty list preserves its empty state', async () => {
  for (const initial of [null, []]) {
    const { api, element, resolve } = fixture(initial);
    const refreshing = api.refreshSavedResults();
    assert.equal(element('#savedResultsStatus').textContent, initial === null ? 'Loading saved results…' : 'No saved results yet.');
    assert.equal(element('#savedResultsTable').hidden, true);
    resolve([]);
    await refreshing;
    assert.equal(element('#savedResultsStatus').textContent, 'No saved results yet.');
    assert.equal(element('#savedResultsStatus').hidden, false);
  }
});

test('a failed refresh retains the loaded results and reports the error', async () => {
  const { api, element, reject } = fixture([{ id: 'saved', state: 'running' }]);
  const rows = element('#savedResultsRows'), writes = rows.writes;
  const refreshing = api.refreshSavedResults();
  reject(new Error('Controller unavailable'));
  await refreshing;
  assert.equal(rows.writes, writes);
  assert.equal(element('#savedResultsTable').hidden, false);
  assert.equal(element('#savedResultsStatus').hidden, false);
  assert.equal(element('#savedResultsStatus').attributes.role, 'alert');
  assert.match(element('#savedResultsStatus').textContent, /Controller unavailable.*Showing the last loaded list/);
  assert.equal(element('#refreshResults').disabled, false);
});


test('Saved results groups only one repetition batch and preserves individual actions', () => {
  const runs = [
    { id: 'a', name: 'Same name', batchId: 'one', repetitions: 3, state: 'completed' },
    { id: 'b', name: 'Same name', batchId: 'one', repetitions: 3, state: 'completed' },
    { id: 'c', name: 'Same name', batchId: 'one', repetitions: 3, state: 'running' },
    { id: 'd', name: 'Same name', batchId: 'two', repetitions: 2, state: 'completed' },
  ];
  const { api, element } = fixture(runs);
  const groups = api.savedResultBatches(runs);
  assert.equal(groups.length, 2);
  assert.equal(groups[0].completed, 2);
  assert.equal(groups[0].active, true);
  assert.match(element('#savedResultsRows').innerHTML, /data-batch-images="one"[^>]+disabled/);
  assert.match(element('#savedResultsRows').innerHTML, /data-result-images="a"/);
  runs[2].state = 'completed';
  api.renderSavedResults();
  assert.doesNotMatch(element('#savedResultsRows').innerHTML, /data-batch-images="one"[^>]+disabled/);
  assert.match(element('#savedResultsRows').innerHTML, /data-batch-images="two"[^>]+disabled/);
});


test('repeated series start collapsed and render every individual result exactly once in archive order', () => {
  const runs = [
    { id: 'first', name: 'Independent', state: 'completed' },
    { id: 'a3', name: 'Same name', batchId: 'alpha', repetitions: 3, iteration: 3, state: 'completed' },
    { id: 'b2', name: 'Same name', batchId: 'beta', repetitions: 2, iteration: 2, state: 'completed' },
    { id: 'a2', name: 'Same name', batchId: 'alpha', repetitions: 3, iteration: 2, state: 'completed' },
    { id: 'middle', state: 'completed' },
    { id: 'a1', name: 'Same name', batchId: 'alpha', repetitions: 3, iteration: 1, state: 'completed' },
    { id: 'b1', name: 'Same name', batchId: 'beta', repetitions: 2, iteration: 1, state: 'completed' },
    { id: 'single', batchId: 'one-run', repetitions: 1, iteration: 1, state: 'completed' },
  ];
  const { element } = fixture(runs);
  const html = element('#savedResultsRows').innerHTML;
  assert.deepEqual([...html.matchAll(/<details[^>]+data-result-batch="([^"]+)"/g)].map(match => match[1]), ['alpha', 'beta']);
  assert.doesNotMatch(html, /<details[^>]*\bopen(?:[\s=>])/);
  const groups = [...html.matchAll(/<details[^>]+data-result-batch="([^"]+)"[\s\S]*?<\/details>/g)];
  assert.match(groups[0][0], /Run 3 of 3[\s\S]*Run 2 of 3[\s\S]*Run 1 of 3/);
  assert.match(groups[1][0], /Run 2 of 2[\s\S]*Run 1 of 2/);
  for (const run of runs) {
    for (const action of ['images', 'download']) assert.equal(html.split(`data-result-${action}="${run.id}"`).length - 1, 1);
    assert.equal(html.split(`data-delete-result="${run.id}"`).length - 1, 1);
  }
  const singleRows = html.replace(/<details[\s\S]*?<\/details>/g, '');
  assert.match(singleRows, /data-result-images="first"[\s\S]*data-result-images="middle"[\s\S]*data-result-images="single"/);
  assert.doesNotMatch(singleRows, /data-result-images="[ab][123]"/);
  assert.ok(html.indexOf('data-result-images="first"') < html.indexOf('data-result-batch="alpha"'));
});

test('series retain incomplete metadata members and remain grouped until their last saved run is deleted', () => {
  const id = 'batch"<&', name = '<img src=x onerror=alert(1)>';
  const runs = [
    { id: 'metadata-partial', batchId: id, name, state: 'unreadable' },
    { id: 'remaining', batchId: id, name, repetitions: 3, iteration: 2, state: 'completed' },
    { id: 'unrelated', name, repetitions: 3, iteration: 1, state: 'completed' },
  ];
  const { api, state, element } = fixture(runs);
  let html = element('#savedResultsRows').innerHTML;
  assert.match(html, /data-result-batch="batch&quot;&lt;&amp;"/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /<details[\s\S]*data-result-images="metadata-partial"[\s\S]*data-result-images="remaining"[\s\S]*<\/details>/);
  assert.match(html, /2 runs · 1 \/ 3 completed · 1 excluded · 1 missing\/unreadable/);
  state.savedResults = runs.slice(1);
  api.renderSavedResults();
  html = element('#savedResultsRows').innerHTML;
  assert.match(html, /<details/);
  assert.match(html, /1 run · 1 \/ 3 completed/);
  assert.match(html, /data-batch-images="batch&quot;&lt;&amp;"[^>]+disabled/);
  state.savedResults = runs.slice(2);
  api.renderSavedResults();
  html = element('#savedResultsRows').innerHTML;
  assert.doesNotMatch(html, /<details/);
  assert.match(html, /data-result-images="unrelated"/);
});

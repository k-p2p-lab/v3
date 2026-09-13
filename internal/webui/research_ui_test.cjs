const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function fixture() {
  const elements = new Map(), statuses = [];
  let parses = 0;
  const element = selector => {
    if (!elements.has(selector)) elements.set(selector, {
      innerHTML: '', value: '', hidden: false, listeners: {},
      addEventListener(type, listener) { this.listeners[type] = listener; },
    });
    return elements.get(selector);
  };
  const sandbox = {
    AbortController, DOMException, setTimeout, clearTimeout,
    KPLResearch: { labels: { frt: 'First receipt' } },
    KPLResearchCompare: {},
    KPLResearchFiles: { parseImport() {
      parses++;
      return { entries: [{ case: 'imported', analysis: { result: { name: 'late import' } } }], curves: [] };
    } },
  };
  vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/research-ui.js'), 'utf8'), sandbox);
  const ui = sandbox.KPLResearchTools.create({
    document: { querySelector: element }, request: async () => [], render: async () => {},
    status: message => statuses.push(message), onJob() {}, getData: () => null, escape: value => String(value ?? ''),
  });
  return { element, statuses, ui, parses: () => parses };
}
const settled = () => new Promise(resolve => setImmediate(resolve));

test('clearing imports cancels a pending file read before it can restore cleared data', async () => {
  const { element, statuses, parses } = fixture();
  let resolve;
  element('#researchFiles').listeners.change({ target: { files: [
    { name: 'late.json', size: 20, text: () => new Promise(yes => { resolve = yes; }) },
  ] } });
  element('#clearResearchImports').listeners.click();
  resolve('{}');
  await settled();
  assert.equal(parses(), 0, 'cleared file was still parsed');
  assert.equal(element('#researchRuns').innerHTML, '');
  assert.equal(statuses.at(-1), 'Imported data cleared.');
});

test('closing the image view cancels a pending import while completed imports remain usable', async () => {
  const { element, ui, parses } = fixture();
  element('#researchFiles').listeners.change({ target: { files: [
    { name: 'complete.json', size: 2, text: async () => '{}' },
  ] } });
  await settled();
  assert.equal(parses(), 1);
  assert.match(element('#researchRuns').innerHTML, /late import/);
  const rows = element('#researchRuns').innerHTML;
  let resolve;
  element('#researchFiles').listeners.change({ target: { files: [
    { name: 'canceled.json', size: 2, text: () => new Promise(yes => { resolve = yes; }) },
  ] } });
  ui.cancel();
  resolve('{}');
  await settled();
  assert.equal(parses(), 1);
  assert.equal(element('#researchRuns').innerHTML, rows);
});

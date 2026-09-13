const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'static/app.js'), 'utf8');
const functions = source.slice(source.indexOf('function escapeHTML('), source.indexOf('$("#scenarioText").value = defaultScenario;'));
function fixture(extra = {}) {
  const elements = new Map();
  const element = selector => {
    if (!elements.has(selector)) elements.set(selector, {
      textContent:'', innerHTML:'', value:'', disabled:false, hidden:false, open:false,
      classList:{toggle(){}}, setAttribute(){},
      showModal(){this.open=true;}, close(){this.open=false;},
    });
    return elements.get(selector);
  };
  const api = {Intl, Date, AbortController, setTimeout, clearTimeout,
    document:{querySelector:element}, localStorage:{getItem:()=>null,setItem(){}}, ...extra};
  vm.createContext(api);
  vm.runInContext(source.slice(0, source.indexOf('const defaultScenario =')) + functions, api);
  const state = vm.runInContext('state', api);
  state.savedResults = [{id:'saved-run', name:'Saved run', state:'completed'}];
  api.showToast = () => {};
  return {api, state, element};
}
test('opening deletion requires no background size request', () => {
  let calls=0;
  const {api,state,element}=fixture({fetch:()=>{calls++;throw new Error('unexpected request');}});
  state.savedResults[0].sourceBytes=1536;
  api.renderSavedResults();
  api.requestResultDeletion('saved-run');
  assert.equal(element('#deleteResultDialog').open,true);
  assert.equal(calls,0);
});

test('deletion finishes without waiting for a slow list refresh', async () => {
  for (const status of [204,404]) {
    let deletions=0, refreshes=0;
    const {api,state,element}=fixture();
    api.api = async (_url,options)=>{
      assert.equal(options.method,'DELETE');assert.ok(options.signal);deletions++;
      if (status===404) throw Object.assign(new Error('already deleted'),{status});
    };
    api.refreshSavedResults=()=>{refreshes++;return new Promise(()=>{});};
    api.requestResultDeletion('saved-run');
    await api.confirmResultDeletion();
    assert.equal(deletions,1);assert.equal(refreshes,1);
    assert.equal(state.deletingResultId,null);
    assert.equal(element('#deleteResultDialog').open,false);
    assert.equal(state.savedResults.length,0);
    assert.equal(state.deletedResultIDs.has('saved-run'),true);
    assert.equal(element('#confirmDeleteResult').disabled,false);
    api.renderRuns([{id:'saved-run',state:'completed'}]);
    assert.match(element('#runList').innerHTML,/No experiments yet/);
  }
});

test('timed-out deletion releases controls and preserves a retryable result', async () => {
  const timers = new Map(); let next=1;
  const {api,state,element} = fixture({
    setTimeout:(fn,delay)=>{const id=next++;timers.set(id,{fn,delay});return id;},
    clearTimeout:id=>timers.delete(id),
  });
  api.api = (_url,options)=>new Promise((_resolve,reject)=>{
    options.signal.addEventListener('abort',()=>reject(Object.assign(new Error('timeout'),{name:'AbortError'})));
  });
  api.refreshSavedResults = ()=>new Promise(()=>{});
  api.requestResultDeletion('saved-run');
  const deleting = api.confirmResultDeletion();
  assert.equal(element('#cancelDeleteResult').disabled,true);
  assert.equal(timers.size,1);
  const timer=[...timers.values()][0];
  assert.equal(timer.delay,30000);
  timer.fn(); await deleting;
  assert.equal(timers.size,0);
  assert.equal(state.deletingResultId,null);
  assert.equal(state.savedResults.length,1);
  assert.equal(state.deletedResultIDs.size,0);
  assert.equal(element('#confirmDeleteResult').disabled,false);
  assert.equal(element('#cancelDeleteResult').disabled,false);
  assert.match(element('#deleteResultError').textContent,/timed out; it may still finish/);
});

test('real download conflicts stay visible and active results cannot be deleted', async () => {
  const {api,state,element} = fixture();
  let calls=0;
  api.api = async ()=>{calls++;throw Object.assign(new Error('downloading'),{status:409});};
  api.refreshSavedResults = ()=>new Promise(()=>{});
  api.requestResultDeletion('saved-run');
  await api.confirmResultDeletion();
  assert.equal(calls,1);
  assert.equal(state.deletedResultIDs.size,0);
  assert.equal(element('#deleteResultDialog').open,true);
  assert.equal(element('#confirmDeleteResult').disabled,false);
  assert.match(element('#deleteResultError').textContent,/being downloaded/);
  state.savedResults[0].state='running';
  await api.confirmResultDeletion();
  assert.equal(calls,1,'active result was sent to the deletion API');
  assert.match(element('#deleteResultError').textContent,/active/);
});

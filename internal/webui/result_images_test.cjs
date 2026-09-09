const test=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const images=require('./static/result-images.js');
const png='data:image/png;base64,iVBORw0KGgo=';
function sample(id='run') {
  return {version:1,result:{id,name:'Example <run>',state:'completed'},asOf:'2026-09-09T00:00:00Z',binSeconds:2,
    metrics:{definition:'session-window-v1',deliveryWindows:['1s'],latencySamples:2,averageLatencyMs:15,p95LatencyMs:20},
    latencyCDF:[{x:10,y:.5},{x:20,y:1}],latencyHistogram:[{x:10,y:1},{x:20,y:1}],timeline:[{at:'2026-09-09T00:00:00Z',publish:2,deliver:4,duplicate:0}],observations:[]};
}
function fixture(api,renderImage=async()=>png,options={}) {
  const elements=new Map();
  const element=id=>{
    if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',open:false,hidden:id==='resultImagesAuth',listeners:{},classList:{toggle(){}},setAttribute(){},removeAttribute(){},insertAdjacentHTML(position,html){this.innerHTML+=html;},
      addEventListener(name,fn){this.listeners[name]=fn;},showModal(){this.open=true;},close(){this.open=false;this.listeners.close?.();}});
    return elements.get(id);
  };
  return {element,ui:images.createUI({api,renderImage,pollInterval:0,...options,document:{querySelector:selector=>element(selector.slice(1))}})};
}
async function settle(predicate) {
  for(let i=0;i<40;i++){if(predicate())return;await new Promise(resolve=>setImmediate(resolve));}
  assert.fail('image request did not settle');
}

test('dashboard has one per-result image dialog and no comparison workspace',()=>{
  const html=fs.readFileSync(__dirname+'/static/index.html','utf8');
  assert.match(html,/<dialog[^>]+id="resultImagesDialog"/);
  assert.match(html,/src="\/result-images.js"/);
  assert.doesNotMatch(html,/analysisPanel|analysisRun|analysisGroup|Add run|Visualize &amp; compare|\/analysis.js/);
  const app=fs.readFileSync(__dirname+'/static/app.js','utf8');
  assert.match(app,/data-result-images=/);
  assert.match(app,/KPLResultImages\?\.open/);
  assert.doesNotMatch(app,/KPLAnalysis|resultAnalysisButton/);
});

test('one result produces fixed images and does not invent absent bandwidth or scores',()=>{
  const data=sample(),charts=images.buildCharts(data);
  assert.deepEqual(charts.map(c=>c.id),['latency-cdf','latency-distribution','message-activity']);
  assert.equal(charts[0].series[0].points.at(-1).y,100);
  assert.equal(charts[2].series[0].points[0].y,1);
  assert.match(charts[0].note,/not network reachability/);
  assert.match(charts[0].source,/Example <run>.*run/);
  data.metrics.latencySamples=0;data.latencyCDF=[];data.latencyHistogram=[];
  assert.equal(images.metricValue(data.metrics,'averageLatencyMs'),null);
  assert.match(images.chartSVG(images.buildCharts(data)[0]),/No eligible observations/);
});

test('optional images preserve control units and separate groups without a filter UI',()=>{
  const data=sample();data.metrics.gossipsubControl=[{direction:'send',controlType:'ihave',rpcs:2,entries:10,messageIds:50},{direction:'recv',controlType:'iwant',rpcs:3}];
  data.bandwidthBinSeconds=2;data.bandwidthTimeline=[{at:data.asOf,sentBytes:1000,receivedBytes:2000,protocols:[]}];
  data.observations=[{at:data.asOf,groups:[{group:'',scoreMean:50,layers:[]},{group:'low',scoreMean:2,layers:[{protocol:'gossipsub',averageDegree:6}]},{group:'high',scoreMean:-4,layers:[{protocol:'gossipsub',averageDegree:3}]}]}];
  const charts=images.buildCharts(data);
  const control=charts.find(c=>c.id==='gossipsub-control');
  assert.equal(control.series[0].points[0].y,2);
  assert.equal(control.series[1].points[1].y,3);
  assert.match(images.chartSVG(control),/IHAVE/);
  assert.equal(charts.find(c=>c.id==='p2p-throughput').series[0].points[0].y,4);
  assert.deepEqual(charts.find(c=>c.id==='peer-scores').series.map(s=>s.name),['high','low']);
});

test('PNG source SVG uses a white background and embeds escaped source and definitions',()=>{
  const chart=images.buildCharts(sample())[0],svg=images.chartSVG(chart);
  assert.match(svg,/fill="#ffffff"/);
  assert.match(svg,/Example &lt;run&gt;/);
  assert.match(svg,/session-window-v1/);
  assert.doesNotMatch(svg,/<run>/);
  for(const points of [[{x:0,y:0}],[{x:1e308,y:1e308}],[{x:-1e308,y:-1e308},{x:1e308,y:1e308}]]) {
    const actual=images.chartSVG({...chart,series:[{name:'numbers',points}],yMax:undefined,zeroX:false});
    assert.doesNotMatch(actual,/NaN|Infinity/);
  }
});

const job = (id='run',state='completed',extra={}) => ({version:1,id:id+'-job',runId:id,state,progress:state==='completed'?100:50,phase:'events.jsonl',processedBytes:1048576,totalBytes:2097152,...extra});
const artifact = id => ({...sample(id),analysisId:id+'-job'});
const completedAPI = async url => {
 const id=url.split('/')[4].split('?')[0];
 return url.includes('/result?') ? artifact(id) : job(id);
};

test('a new result starts once, polls byte progress, and downloads the completed snapshot',async()=>{
 const calls=[],updates=[];let poll=0;
 const {ui,element}=fixture(async(url,options)=>{
  calls.push({url,options});
  if(url.includes('/result?')) return artifact('one');
  if(options.method==='POST')return job('one','queued');
  return ++poll===1 ? job('one','idle') : job('one',poll===2?'running':'completed');
 },undefined,{onJob:value=>updates.push(value.state)});
 await ui.open('one');
 assert.equal(calls.filter(c=>c.options.method==='POST').length,1);
 assert.deepEqual(updates,['queued','running','completed']);
 assert.ok(calls.every(c=>c.options.cache==='no-store'));
 assert.ok(calls.every(c=>!c.url.endsWith('/analysis')));
 assert.equal(element('resultImagesDialog').open,true);
 assert.equal(element('resultImagesName').textContent,'Example <run>');
 assert.equal((element('resultImagesGrid').innerHTML.match(/<figure/g)||[]).length,3);
 assert.match(element('downloadResultAnalysis').href,/one\/result\?jobId=one-job$/);
 assert.equal(element('downloadResultAnalysis').hidden,false);
 assert.equal(element('refreshResultImages').hidden,false);
 assert.match(element('resultImagesStatus').textContent,/3 images ready/);
 assert.match(images.jobDescription(job()),/Analysis complete/);
 assert.match(images.jobDescription(job('one','running')),/1 \/ 2 MiB read/);
});

test('closing detaches from server analysis and reopening completed work does not POST',async()=>{
 let resolveStatus;const calls=[];
 const {ui,element}=fixture((url,options)=>{
  calls.push({url,options});
  if(calls.length===1)return new Promise(resolve=>resolveStatus=resolve);
  return completedAPI(url);
 });
 const first=ui.open('one');
 element('resultImagesDialog').close();
 assert.equal(calls[0].options.signal.aborted,true);
 const second=ui.open('two');
 resolveStatus(job('one','running'));await first;await second;
 assert.match(element('resultImagesGrid').innerHTML,/two-latency-cdf.png/);
 assert.doesNotMatch(element('resultImagesGrid').innerHTML,/one-latency-cdf.png/);
 assert.ok(calls.every(call=>!call.options.method));
 ui.remove('two');assert.equal(element('resultImagesDialog').open,false);
 assert.equal(element('resultImagesGrid').innerHTML,'');
});

test('closing during PNG conversion preserves server artifacts and rejects late gallery writes',async()=>{
 let finish;
 const {ui,element}=fixture(completedAPI,()=>new Promise(resolve=>finish=resolve));
 const work=ui.open('run');await settle(()=>finish);
 element('resultImagesDialog').close();finish(png);await work;
 assert.equal(element('resultImagesGrid').innerHTML,'');
});

test('status failures reconnect without resubmitting jobs; saved failures require explicit retry',async()=>{
 let failed=true,posts=0;
 const {ui,element}=fixture(async(url,options)=>{
  if(url.includes('/result?'))return artifact('run');
  if(options.method==='POST'){posts++;failed=false;}
  return job('run',failed?'failed':'completed',{error:'Unreadable event log'});
 });
 await ui.open('run');assert.equal(posts,0);assert.equal(element('retryResultImages').hidden,false);
 assert.match(element('resultImagesStatus').textContent,/Unreadable event log/);
 element('retryResultImages').listeners.click();await settle(()=>element('resultImagesGrid').innerHTML);
 assert.equal(posts,1);
 let calls=0;
 const transient=fixture(async(url,options)=>{calls++;if(calls===1)throw Error('Connection lost');assert.ok(!options.method);return completedAPI(url);});
 await transient.ui.open('run');
 transient.element('retryResultImages').listeners.click();await settle(()=>transient.element('resultImagesGrid').innerHTML);
});

test('explicit refresh requests a new snapshot and authentication failures accept a token',async()=>{
 const methods=[];
 const refresh=fixture(async(url,options)=>{methods.push([url,options.method]);return completedAPI(url);});
 await refresh.ui.open('run');
 refresh.element('refreshResultImages').listeners.click();await settle(()=>methods.some(([url,method])=>url.endsWith('?refresh=1')&&method==='POST'));
 await settle(()=>refresh.element('resultImagesGrid').innerHTML);
 let saved='',started=false;
 const auth=fixture(async(url,options)=>{
  if(url.includes('/result?'))return artifact('run');
  if(options.method==='POST'){
   if(!saved)throw Object.assign(Error('valid bearer token required'),{status:401});
   started=true;return job();
  }
  return job('run',started?'completed':'idle');
 },undefined,{saveToken:value=>saved=value});
 await auth.ui.open('run');assert.equal(auth.element('resultImagesAuth').hidden,false);
 auth.element('resultImagesToken').value='test-token';auth.element('retryResultImages').listeners.click();
 await settle(()=>auth.element('resultImagesGrid').innerHTML);
 assert.equal(saved,'test-token');
});

test('corrupt artifacts and PNG errors stay retryable without discarding saved analysis access',async()=>{
 for(const data of [{...artifact('run'),analysisId:'wrong'},{...artifact('run'),observations:[{at:sample().asOf,groups:null}]}]){
  const {ui,element}=fixture(async url=>url.includes('/result?')?data:job());
  await ui.open('run');assert.equal(element('retryResultImages').hidden,false);assert.equal(element('resultImagesGrid').innerHTML,'');
 }
 const {ui,element}=fixture(completedAPI,async()=>{throw Error('Canvas unavailable');});
 await ui.open('run');assert.match(element('resultImagesStatus').textContent,/Canvas unavailable/);
 assert.equal(element('retryResultImages').hidden,false);assert.equal(element('downloadResultAnalysis').hidden,false);
});

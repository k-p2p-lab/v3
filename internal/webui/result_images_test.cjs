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
function fixture(api,renderImage=async()=>png) {
  const elements=new Map();
  const element=id=>{
    if(!elements.has(id))elements.set(id,{innerHTML:'',textContent:'',open:false,hidden:false,listeners:{},classList:{toggle(){}},setAttribute(){},
      addEventListener(name,fn){this.listeners[name]=fn;},showModal(){this.open=true;},close(){this.open=false;this.listeners.close?.();}});
    return elements.get(id);
  };
  return {element,ui:images.createUI({api,renderImage,document:{querySelector:selector=>element(selector.slice(1))}})};
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

test('Images opens just the selected result and uses identical PNGs for preview and download',async()=>{
  const calls=[];
  const {ui,element}=fixture(async(url,options)=>{calls.push({url,options});return sample('one');});
  assert.equal(calls.length,0);
  await ui.open('one');
  assert.equal(calls.length,1);assert.match(calls[0].url,/\/one\/analysis$/);
  assert.equal(calls[0].options.cache,'no-store');
  assert.equal(element('resultImagesDialog').open,true);
  assert.equal(element('resultImagesName').textContent,'Example <run>');
  assert.equal((element('resultImagesGrid').innerHTML.match(/<figure/g)||[]).length,3);
  assert.equal((element('resultImagesGrid').innerHTML.match(/href="data:image\/png;base64,iVBORw0KGgo="/g)||[]).length,6);
  assert.match(element('resultImagesGrid').innerHTML,/one-latency-cdf.png/);
  assert.match(element('resultImagesStatus').textContent,/3 images/);
});

test('closing or switching results aborts requests and discards late responses',async()=>{
  const pending=[];
  const {ui,element}=fixture((url,options)=>new Promise(resolve=>pending.push({options,resolve})));
  const first=ui.open('first');
  element('resultImagesDialog').close();
  assert.equal(pending[0].options.signal.aborted,true);
  const second=ui.open('second');
  pending[0].resolve(sample('first'));await first;
  assert.equal(element('resultImagesGrid').innerHTML,'');
  pending[1].resolve(sample('second'));await second;
  assert.match(element('resultImagesGrid').innerHTML,/second-latency-cdf.png/);
  assert.doesNotMatch(element('resultImagesGrid').innerHTML,/first-latency-cdf.png/);
  ui.remove('second');assert.equal(element('resultImagesDialog').open,false);
  assert.equal(element('resultImagesGrid').innerHTML,'');
});

test('closing during PNG conversion prevents a detached gallery from being installed',async()=>{
  let finish;
  const {ui,element}=fixture(async()=>sample(),()=>new Promise(resolve=>finish=resolve));
  const work=ui.open('run');await settle(()=>finish);
  element('resultImagesDialog').close();finish(png);await work;
  assert.equal(element('resultImagesGrid').innerHTML,'');
});

test('failed requests, malformed data and PNG errors remain visible and retryable',async()=>{
  for(const response of [async()=>{throw Error('Saved result not found');},async()=>({...sample(),result:{id:'wrong'}}),async()=>({...sample(),observations:[{at:sample().asOf,groups:[{group:'bad',layers:{}}]}]})]){
    let calls=0;
    const {ui,element}=fixture(async()=>++calls===1 ? response() : sample());
    await ui.open('run');assert.equal(element('retryResultImages').hidden,false);assert.equal(element('resultImagesGrid').innerHTML,'');
    element('retryResultImages').listeners.click();await settle(()=>element('retryResultImages').hidden && element('resultImagesGrid').innerHTML);
  }
  const {ui,element}=fixture(async()=>sample(),async()=>{throw Error('Canvas unavailable');});
  await ui.open('run');assert.match(element('resultImagesStatus').textContent,/Canvas unavailable/);assert.equal(element('retryResultImages').hidden,false);
});

const test = require('node:test');
const assert = require('node:assert/strict');
const analysis = require('./static/analysis.js');

function sample(id = 'run') {
  return {version:1,result:{id,name:`Name ${id}`,state:'completed'},asOf:'2026-09-08T00:00:00Z',
    eventCount:3,untimedEvents:0,binSeconds:1,observationCount:0,observations:[],
    metrics:{definition:'cohort-v1',published:1,delivered:2,duplicates:0,latencySamples:2,averageLatencyMs:15,p95LatencyMs:20,deliveryRatioAvailable:true,reachability:1,deliveryRatioUpperBound:1,duplicateSamples:2,averageDuplicates:0},
    latencyCDF:[{x:10,y:.5},{x:20,y:1}],latencyHistogram:[{x:10,y:1},{x:20,y:1}],timeline:[{at:'2026-09-08T00:00:00Z',publish:1,deliver:2,duplicate:0}]};
}

function fixture(api) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, {value:id === 'analysisProtocol' ? 'gossipsub' : '',max:'0',innerHTML:'',textContent:'',hidden:false,disabled:false,
      listeners:{},addEventListener(type,fn){this.listeners[type]=fn;},classList:{toggle(){}},setAttribute(){},scrollIntoView(){}});
    return elements.get(id);
  };
  const ui = analysis.createUI({api,document:{querySelector:selector => element(selector.slice(1))}});
  return {ui,element};
}
async function settled(predicate) {
  for (let i=0;i<40;i++) {if (predicate()) return;await new Promise(resolve => setImmediate(resolve));}
  assert.fail('analysis did not settle');
}

test('missing metrics remain N/A while measured zero stays zero', () => {
  for (const key of ['averageLatencyMs','p95LatencyMs','averageDuplicates','reachability']) assert.equal(analysis.metricValue({[key]:0},key),null);
  assert.equal(analysis.metricValue({latencySamples:1,averageLatencyMs:0},'averageLatencyMs'),0);
  assert.equal(analysis.metricValue({duplicateSamples:1,averageDuplicates:0},'averageDuplicates'),0);
  assert.equal(analysis.metricValue({definition:'dispatch-cohort-v1',deliveryRatioAvailable:true,reachability:1,deliveryRatioUpperBound:0},'deliveryRatioUpperBound'),null);
  assert.equal(analysis.metricValue({definition:'session-window-v1',deliveryRatioAvailable:true,reachability:.5,deliveryRatioUpperBound:.8},'deliveryRatioUpperBound'),.8);
  assert.equal(analysis.difference(null,0),null);
  assert.equal(analysis.difference(0,2),-2);
});

test('timeline gaps are zero event rates and bins preserve counts', () => {
  const data = {binSeconds:2,timeline:[{at:'2026-09-08T00:00:00Z',publish:4},{at:'2026-09-08T00:00:06Z',publish:6}]};
  const points = analysis.trafficPoints(data,'publish');
  let total = 0;
  for (let i=1;i<points.length;i++) total += (points[i].x - points[i-1].x) * points[i-1].y;
  assert.equal(total,10);
  assert.ok(points.some(p => p.x === 2 && p.y === 0));
  assert.deepEqual(analysis.trafficPoints({...data,binSeconds:0},'publish'),[]);
});

test('observation groups preserve missing-data gaps without fabricating zeros', () => {
  const data = {observations:[
    {at:'2026-09-08T00:00:00Z',groups:[{group:'workers',scoreMean:0}]},
    {at:'2026-09-08T00:00:05Z',groups:[]},
    {at:'2026-09-08T00:00:10Z',groups:[{group:'workers',scoreMean:-2}]}]};
  assert.deepEqual(analysis.observationPoints(data,'workers',g => g.scoreMean),[{x:0,y:0},{x:5,y:null},{x:10,y:-2}]);
});

test('SVG handles empty, constant, negative and escaped data and includes export legends', () => {
  const chart = {title:'<script>alert(1)</script>',xLabel:'time',yLabel:'score',series:[{name:'<img onerror="bad">',points:[{x:0,y:0},{x:1,y:null},{x:2,y:-2}]}]};
  const svg = analysis.chartSVG(chart);
  assert.ok(!svg.includes('<script>') && !svg.includes('<img'));
  assert.ok(svg.includes('&lt;script&gt;') && svg.includes('&lt;img'));
  assert.ok(!/NaN|Infinity/.test(svg));
  assert.match(svg,/stroke-width="2"/);
  assert.match(analysis.chartSVG({...chart,series:[{name:'empty',points:[{x:0,y:null}]}]}),/No eligible observations/);
  assert.ok(!/NaN|Infinity/.test(analysis.chartSVG({...chart,series:[{name:'constant',points:[{x:0,y:0}]}]})));
});

test('CSV escapes spreadsheet formulas and quotes; unavailable data is blank', () => {
  const data=sample(); data.result.name='=DANGEROUS,"quoted"';data.metrics.latencySamples=0;
  const csv=analysis.summaryCSV([data]);
  assert.match(csv,/"'=DANGEROUS,""quoted"""/);
  assert.match(csv,/,"","","0",/);
  assert.ok(csv.endsWith('\r\n'));
});

test('analysis requests are sequential, capped at four, and integrated with charts', async () => {
  const requests=[];
  const {ui,element}=fixture((url,options)=>new Promise(resolve => requests.push({url,options,resolve})));
  ui.setResults(['one','two','three','four','five'].map(id=>({id,state:'completed'})));
  for (const id of ['one','two','three','four','five']) ui.add(id);
  assert.equal(requests.length,1);
  assert.match(element('analysisStatus').textContent,/maximum four/);
  for (const [index,id] of ['one','two','three','four'].entries()) {
    await settled(()=>requests.length===index+1);
    assert.ok(requests[index].url.endsWith(`/${id}/analysis`));
    assert.equal(requests[index].options.cache,'no-store');
    assert.ok(requests[index].options.signal);
    requests[index].resolve(sample(id));
  }
  await settled(()=>!element('refreshAnalysis').disabled);
  assert.equal(element('analysisContent').hidden,false);
  assert.equal(element('exportAnalysis').disabled,false);
  assert.match(element('analysisSummary').innerHTML,/Name one/);
  assert.match(element('analysisComparisonCharts').innerHTML,/Propagation latency CDF/);
  assert.match(element('analysisDetailCharts').innerHTML,/Degree distribution/);
  assert.match(element('analysisObservationNote').textContent,/no stored topology/);
  assert.match(element('analysisRuns').innerHTML,/Baseline/);
});

test('removal aborts requests and ignores late responses even after the same run is re-added', async () => {
  const requests=[];
  const {ui,element}=fixture((url,options)=>new Promise(resolve=>requests.push({resolve,options})));
  ui.add('run'); ui.remove('run');
  assert.equal(requests[0].options.signal.aborted,true);
  ui.add('run');
  const stale=sample();stale.result.name='STALE';requests[0].resolve(stale);
  await settled(()=>requests.length===2);
  assert.equal(element('analysisContent').hidden,true);
  requests[1].resolve(sample());
  await settled(()=>!element('refreshAnalysis').disabled);
  assert.ok(!element('analysisSummary').innerHTML.includes('STALE'));
  ui.setResults([]);
  assert.equal(element('analysisContent').hidden,true);
  assert.equal(element('exportAnalysis').disabled,true);
});

test('failed refresh preserves its dated snapshot and remains retryable', async () => {
  let calls=0;
  const {ui,element}=fixture(async()=>{calls++; if(calls===2) throw new Error('corrupt events');return sample();});
  ui.add('run');await settled(()=>!element('refreshAnalysis').disabled);
  element('refreshAnalysis').listeners.click();
  await settled(()=>!element('refreshAnalysis').disabled);
  assert.match(element('analysisRuns').innerHTML,/corrupt events/);
  assert.equal(element('analysisContent').hidden,false);
  assert.match(element('analysisStatus').textContent,/Previously loaded snapshots/);
  element('refreshAnalysis').listeners.click();
  await settled(()=>calls===3 && !element('refreshAnalysis').disabled);
  assert.ok(!element('analysisRuns').innerHTML.includes('corrupt events'));
});

test('malformed responses show errors without installing invalid chart data', async () => {
  const {ui,element}=fixture(async()=>({version:1,result:{id:'another'}}));
  ui.add('run');await settled(()=>!element('refreshAnalysis').disabled);
  assert.match(element('analysisRuns').innerHTML,/Unexpected analysis response/);
  assert.equal(element('analysisContent').hidden,true);
  assert.equal(element('exportAnalysis').disabled,true);
});


test('churn comparison includes starting delivery and coverage without fabricating legacy bounds', () => {
  assert.equal(analysis.metricRange({definition:'dispatch-cohort-v1',deliveryRatioAvailable:true,reachability:1,deliveryRatioUpperBound:0},'reachability','deliveryRatioUpperBound'),'100%');
  assert.equal(analysis.metricRange({stableCoverage:0,stableCoverageUpperBound:0},'stableCoverage','stableCoverageUpperBound'),'N/A');
  assert.equal(analysis.metricRange({stableCoverageAvailable:true,stableCoverage:.6,stableCoverageUpperBound:.8},'stableCoverage','stableCoverageUpperBound'),'60 – 80%');
});

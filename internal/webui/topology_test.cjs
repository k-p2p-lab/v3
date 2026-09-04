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

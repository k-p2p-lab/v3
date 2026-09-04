// Run with: node --test internal/webui/topology_test.cjs
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, 'static/app.js'), 'utf8');
const functions = source.slice(source.indexOf('function escapeHTML('), source.indexOf('$("#scenarioText").value = defaultScenario;'));
function context(extra = {}) {
  const sandbox = { Intl, Date, ...extra };
  vm.createContext(sandbox);
  vm.runInContext(functions, sandbox);
  return sandbox;
}
const plain = (value) => JSON.parse(JSON.stringify(value));
const peer = (id, agentId = 'a', state = 'ready') => ({ id, agentId, state });
const memory = () => ({ agents: new Map(), columns: 0, cellHeight: 260 });

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

test('churn reuses vacant slots without moving surviving nodes or other Agent groups', () => {
  const api=context(), cache=memory(), agents=[{id:'a',capacity:20},{id:'b',capacity:20}];
  const initial=[peer('a1'),peer('a2'),peer('a3'),peer('b1','b')];
  const first=api.layoutTopology(initial,agents,cache,1280);
  const reordered=api.layoutTopology([...initial].reverse(),[...agents].reverse(),cache,800);
  for(const node of initial) assert.deepEqual(plain(reordered.positions.get(node.id)),plain(first.positions.get(node.id)));
  const changed=api.layoutTopology([peer('new'),peer('a1'),peer('a3'),peer('b1','b')],agents,cache,1280);
  for(const id of ['a1','a3','b1']) assert.deepEqual(plain(changed.positions.get(id)),plain(first.positions.get(id)));
  assert.deepEqual(plain(changed.positions.get('new')),plain(first.positions.get('a2')));
  const added=api.layoutTopology([peer('new'),peer('a1'),peer('a3'),peer('b1','b'),peer('c1','c')],[...agents,{id:'c',capacity:20}],cache,1280);
  assert.deepEqual(plain(added.positions.get('b1')),plain(first.positions.get('b1')));
});

test('occupied-slot growth contains arrivals, and moving a node releases its old Agent slot', () => {
  const api=context(), cache=memory(), agents=[{id:'a',capacity:96},{id:'b',capacity:20}];
  const initial=api.layoutTopology([peer('one'),peer('other','b')],agents,cache,1000);
  const nodes=[peer('one'),peer('other','b'),...Array.from({length:95},(_,i)=>peer(`n${i}`))];
  const full=api.layoutTopology(nodes,agents,cache,1000);
  assert.deepEqual(plain(full.positions.get('other')),plain(initial.positions.get('other')));
  const group=full.groups.find(g=>g.id==='a');
  for(const node of nodes.filter(n=>n.agentId==='a')) {
    const point=full.positions.get(node.id);
    assert.ok(point.x>group.x && point.x<group.x+group.width);
    assert.ok(point.y>group.y && point.y+25<group.y+group.height);
  }
  api.layoutTopology([peer('one','b')],agents,cache,1000);
  assert.equal(cache.agents.get('a').slots.has('one'),false);
  assert.equal(cache.agents.get('b').slots.has('one'),true);
});

test('configured capacity does not create empty canvas and actual slot growth never shrinks on churn', () => {
  const api=context(), cache=memory(), agents=Array.from({length:7},(_,i)=>({id:`a${i}`,capacity:10000}));
  const nodes=[peer('one','a0'),peer('two','a1'),peer('three','a6')];
  const sparse=api.layoutTopology(nodes,agents,cache,1280);
  assert.ok(sparse.bounds.height<900);
  assert.ok(sparse.groups.every(group=>group.height<300));
  const full=api.layoutTopology([...nodes,...Array.from({length:70},(_,i)=>peer(`n${i}`,'a0'))],agents,cache,1280);
  assert.ok(full.bounds.height>sparse.bounds.height);
  const after=api.layoutTopology(nodes,agents,cache,1280);
  assert.deepEqual(plain(after.bounds),plain(full.bounds));
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

test('rendered graph controls preserve selection and camera while filtering and streaming churn', () => {
  const ids=new Map();
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
  document={activeElement:null,querySelector:(selector)=>ids.get(selector.slice(1)),querySelectorAll:(selector)=>ids.get('topology').querySelectorAll(selector),createElement:(tag)=>new Element(tag),createElementNS:(_,tag)=>new Element(tag)};
  const sandbox={document,Intl,Date,localStorage:{getItem:()=>null,setItem(){}}};
  vm.createContext(sandbox);
  vm.runInContext(source.slice(0,source.indexOf('const defaultScenario ='))+functions,sandbox);
  const state=vm.runInContext('state',sandbox);
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
  assert.deepEqual(plain(state.topology.camera),camera);
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
});

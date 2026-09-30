/* Exercise the actual ES5 dashboard with a small DOM and read-only transport. */
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
const assets = path.join(root, 'cmd/yachtsense-link-emulator/dashboard');
const html = fs.readFileSync(path.join(assets, 'index.html'), 'utf8');
const script = fs.readFileSync(path.join(assets, 'status.js'), 'utf8');
class Element {
  constructor(tag) { this.tagName=tag; this.childNodes=[]; this.value=''; this.listeners={}; this._text=''; }
  appendChild(child) { this.childNodes.push(child); return child; }
  removeChild(child) { this.childNodes.splice(this.childNodes.indexOf(child),1); return child; }
  get firstChild() { return this.childNodes[0] || null; }
  set textContent(value) { this._text=String(value); this.childNodes=[]; }
  get textContent() { return this._text+this.childNodes.map(n=>n.textContent).join(' '); }
  addEventListener(name, handler) { this.listeners[name]=handler; }
}
function fixture() {
  return {ok:true,ready:true,stale:false,emulator:{running:true,version:'1.2.2',raynet:'eth0.3',cidr:'198.18.0.1/21',axiom:'198.18.3.234',discovery:'builtin',relay:true},router:{
    profile:'onWiFi',internet:{links:[]},gps:{},wifi:[],vpn:[],rms:'connected',vxlan:[{name:'vxlan1',up:true,vni:404}],logs:[],
    devices:[{name:'Axiom',ip:'198.18.3.234',connection:'RayNet',state:'observed'},
      {name:'Cerbo GX',ip:'198.18.5.98',connection:'RayNet',state:'reachable'},
      {name:'Phone',ip:'192.168.40.2',connection:'Wi-Fi',state:'associated'},
      {name:'Wired device',ip:'192.168.40.3',connection:'LAN',state:'known'}]
  }};
}
function start(data) {
  const ids={};
  for (const match of html.matchAll(/\bid="([^"]+)"/g)) ids[match[1]]=new Element('div');
  const state={data,requests:[],poll:null};
  const context={console,document:{getElementById:id=>ids[id]||null,createElement:tag=>new Element(tag)},
    window:{location:{hostname:'192.168.40.1'},setInterval:f=>{state.poll=f;}},
    XMLHttpRequest:function(){
      this.open=(method,url)=>state.requests.push([method,url]);
      this.send=()=>{this.readyState=4;this.status=200;this.responseText=JSON.stringify(state.data);this.onreadystatechange();};
    }};
  vm.runInNewContext(script,context,{timeout:1000});
  return {ids,state};
}
let count=0;
function test(name,fn) { fn();count++;console.log('PASS '+name); }
test('RayNet lives under Services, with no standalone card',()=>{
  assert(!/<h2>RayNet<\/h2>/.test(html));
  assert(!html.includes('id="emulator"')&&!html.includes('id="emulator-state"'));
  const {ids}=start(fixture());
  assert.equal(ids.services.childNodes[0].childNodes[0].childNodes[0].textContent,'RayNet');
  assert(ids.services.textContent.includes('eth0.3 · 198.18.0.1/21'));
  assert(ids.services.textContent.includes('Running'));
});
test('relay implementation is hidden but supplied telemetry is not mutated',()=>{
  for(const discovery of ['builtin','avahi','direct']){
    const data=fixture();data.emulator.discovery=discovery;
    const {ids}=start(data);
    assert(!/Built-in relay|Avahi|Active/.test(ids.services.textContent));
    assert.equal(data.emulator.discovery,discovery);assert.equal(data.emulator.relay,true);
  }
});
test('Axiom and Cerbo GX appear once in Devices, not Services',()=>{
  const {ids}=start(fixture());
  assert.equal(ids.devices.childNodes.length,4);
  for(const term of ['Axiom','Cerbo GX','198.18.3.234','198.18.5.98']){
    assert(!ids.services.textContent.includes(term));
    assert.equal(ids.devices.textContent.split(term).length-1,1);
  }
});
test('Wi-Fi, wired and RayNet device classes are preserved',()=>{
  const {ids}=start(fixture());
  for(const term of ['Wi-Fi','LAN','RayNet','Known','Online'])assert(ids.devices.textContent.includes(term));
});
test('repeated polling does not duplicate service or device rows',()=>{
  const {ids,state}=start(fixture());state.poll();state.poll();
  assert.equal(ids.services.childNodes.length,3);assert.equal(ids.devices.childNodes.length,4);
  assert(state.requests.every(([method,url])=>method==='GET'&&url==='/api/status'));
});
test('missing and stale service data is Unknown, not Offline',()=>{
  for(const missing of [true,false]){
    const data=fixture();if(missing)delete data.emulator;else data.stale=true;
    const {ids}=start(data);const row=ids.services.childNodes[0];
    assert(row.textContent.includes('Unknown'));assert(!row.textContent.includes('Offline'));
  }
});
test('explicit stopped service is Offline and RMS/VXLAN remain visible',()=>{
  const data=fixture();data.emulator.running=false;const {ids}=start(data);
  assert(ids.services.childNodes[0].textContent.includes('Offline'));
  assert(ids.services.textContent.includes('RMS')&&ids.services.textContent.includes('vxlan1 · VXLAN'));
});
test('device search and management destination are unchanged',()=>{
  const {ids}=start(fixture());ids.search.value='Cerbo';ids.search.listeners.input();
  assert.equal(ids.devices.childNodes.length,1);assert(ids.devices.textContent.includes('Cerbo GX'));
  assert.equal(ids.manage.href,'https://192.168.40.1/services/yachtsense-link-emulator');
});
console.log(`${count} public dashboard UI tests passed`);

/* Read-only dashboard; ES5 syntax for the embedded MFD browser. */
(function () {
 "use strict";
 var last = null, devices = [], pending = false;
 function el(id) { return document.getElementById(id); }
 function text(node, value) { node.textContent = value == null ? "" : String(value); return node; }
 function make(tag, cls, value) { var n=document.createElement(tag); if(cls)n.className=cls; if(value!=null)text(n,value); return n; }
 function clear(id) { var n=el(id); while(n.firstChild)n.removeChild(n.firstChild); return n; }
 function arr(v) { return Array.isArray(v) ? v : []; }
 function valid(v) { return v!==null && v!==undefined && v!=="" && v!=="N/A" && v!=="unavailable"; }
 function number(v) { return typeof v==="number" && isFinite(v); }
 function badge(label, state) { return make("span", "badge "+(state||"off"),label); }
 function metric(parent,label,value) { if(!valid(value))return; var n=make("div","metric");n.appendChild(make("span","",label));n.appendChild(make("b","",value));parent.appendChild(n); }
 function seconds(v) { if(!number(v))return null;v=Math.max(0,Math.floor(v));var h=Math.floor(v/3600),m=Math.floor(v%3600/60);return (h ? h+"h " : "")+m+"m"; }
 function bytes(v) { if(!number(v))return null;var u=["B","KB","MB","GB"],i=0;while(v>=1024&&i<3){v/=1024;i++;}return v.toFixed(i?1:0)+" "+u[i]; }
 function details(parent, values) { var m=make("div","metrics");for(var i=0;i<values.length;i++)metric(m,values[i][0],values[i][1]);if(m.childNodes.length)parent.appendChild(m); }
 function row(parent,title,sub,label,state) {var n=make("div","row"),l=make("div");l.appendChild(make("div","row-title",title));if(sub)l.appendChild(make("div","secondary",sub));n.appendChild(l);if(label)n.appendChild(badge(label,state));parent.appendChild(n);return l;}
 function showInternet(v) {
  v=v||{};var root=clear("links"),shown=0,active=0;
  arr(v.links).forEach(function(l){
   if(l.disabled || l.kind==="mobile" && l.present===false)return;
   var on=l.state==="active";if(on)active++;
   var n=make("div","link"+(on?" active":"")),top=make("div","link-top");
   top.appendChild(make("div","name",l.name||l.device));
   top.appendChild(badge(on?"Online":l.state==="standby"?"Standby":l.state==="offline"?"Offline":"Unknown",on?"good":"off"));n.appendChild(top);
   if(l.label && l.label!==l.name)n.appendChild(make("div","sub",l.label));
   details(n,[["RSSI",number(l.rssi)?l.rssi+" dBm":null],["SIM",l.sim],["IP",l.ip],["Public IP",l.public_ip]]);
   root.appendChild(n);shown++;
  });
  clear("internet-state");
  if(!shown)root.appendChild(make("div","sub","No connection"));
 }
 function showGPS(g) {
  g=g||{};var root=clear("gps"),head=clear("gps-state"),fix=g.state==="fix";
  head.appendChild(badge(fix?"Fix":g.state==="stale"?"Stale":g.state==="no_fix"?"No fix":"Unknown",fix?"good":"off"));
  if(fix && number(g.latitude)&&number(g.longitude))root.appendChild(make("div","gps-position",g.latitude.toFixed(6)+", "+g.longitude.toFixed(6)));
  if(number(g.satellites))details(root,[["Satellites",g.satellites]]);
  arr(g.fences).forEach(function(f){if(f.state==="disabled")return;row(root,f.name||"Geofence",null,f.state==="inside"?"Inside":f.state==="outside"?"Outside":"Unknown",f.state==="inside"?"good":"off");});
 }
 function showWifi(list) {
  var root=clear("wifi");list=arr(list);el("wifi-card").hidden=!list.length;
  list.forEach(function(w){var sub=[];if(w.band)sub.push(w.band);if(number(w.channel))sub.push("Ch "+w.channel);if(number(w.clients))sub.push(w.clients+" clients");row(root,w.ssid||w.name,sub.join(" · "),w.up?"Online":"Offline",w.up?"good":"off");});
 }
 function showVPN(list) {
  var root=clear("vpn");list=arr(list);el("vpn-card").hidden=!list.length;
  list.forEach(function(v){var on=v.state==="connected"||v.state==="recent_handshake",label=on?"Connected":v.state==="connecting"?"Connecting":v.state==="disconnected"?"Offline":"Unknown";
   if(v.kind==="WireGuard"&&v.state==="recent_handshake")label="Recent handshake";
   var l=row(root,[v.name,v.kind].filter(Boolean).join(" · "),null,label,on?"good":"off");details(l,[["Time",seconds(v.uptime)],["RX",bytes(v.rx_bytes)],["TX",bytes(v.tx_bytes)]]);
  });
 }
 function showServices(v, e, stale) {
  var root=clear("services"),raynet=[];
  // RayNet describes the service/interface; individual hosts belong in Devices.
  if(valid(e.raynet))raynet.push(e.raynet);
  if(valid(e.cidr))raynet.push(e.cidr);
  var known=!stale && typeof e.running==="boolean";
  row(root,"RayNet",raynet.join(" · "),known?(e.running?"Running":"Offline"):"Unknown",known&&e.running?"good":"off");
  row(root,"RMS",null,v.rms==="connected"?"Connected":v.rms==="disconnected"?"Offline":"Unknown",v.rms==="connected"?"good":"off");
  arr(v.vxlan).forEach(function(x){var l=row(root,(x.name||"VXLAN")+" · VXLAN",null,x.up?"Up":"Down",x.up?"good":"off");details(l,[["VNI",x.vni],["RX",bytes(x.rx_bytes)],["TX",bytes(x.tx_bytes)]]);});
 }
 function showDevices() {
  var root=clear("devices"),q=el("search").value.toLowerCase(),visible=0;text(el("device-count"),devices.length);
  devices.forEach(function(d){if([d.name,d.ip,d.mac,d.ssid,d.connection].join(" ").toLowerCase().indexOf(q)<0)return;
   var r=make("tr"),name=make("td","",d.name||d.mac||"—");r.appendChild(name);r.appendChild(make("td","",d.ip||"—"));
   var c=make("td","",d.connection||"LAN");if(d.ssid)c.appendChild(make("div","secondary",d.ssid));r.appendChild(c);
   var st=make("td"),on=d.state==="associated"||d.state==="reachable"||d.state==="observed";st.appendChild(badge(on?"Online":d.state==="known"?"Known":"Seen",on?"good":"off"));r.appendChild(st);root.appendChild(r);visible++;
  });if(!visible){var r=make("tr"),n=make("td","sub","No devices");n.colSpan=4;r.appendChild(n);root.appendChild(r);}
 }
 function render(payload) {
  if(!payload || !payload.ok)throw new Error("Status unavailable");
  var v=payload.router||{},e=payload.emulator||{};
  if(!payload.ready){text(el("notice"),"Loading…");el("notice").hidden=false;return;}
  last=payload;el("content").hidden=false;el("notice").hidden=!payload.stale;if(payload.stale)text(el("notice"),"Status unavailable");
  showInternet(v.internet);showGPS(v.gps);showWifi(v.wifi);showVPN(v.vpn);showServices(v,e,payload.stale);
  el("profile-card").hidden=!valid(v.profile);text(el("profile"),v.profile);
  devices=arr(v.devices);showDevices();text(el("logs"),arr(v.logs).join("\n")||"No entries");
  text(el("version"),"v"+(e.version||"")+(v.firmware?" · "+v.firmware:""));
  var t=new Date(v.observed_at);text(el("updated"),isNaN(t.getTime())?"":t.toLocaleTimeString());
 }
 function poll() {
  if(pending)return;pending=true;var xhr=new XMLHttpRequest();xhr.open("GET","/api/status",true);xhr.timeout=6000;
  xhr.onreadystatechange=function(){if(xhr.readyState!==4)return;pending=false;try{if(xhr.status!==200)throw new Error();render(JSON.parse(xhr.responseText));}catch(_){text(el("notice"),"Status unavailable");el("notice").hidden=false;}};
  xhr.ontimeout=xhr.onerror=function(){pending=false;text(el("notice"),"Status unavailable");el("notice").hidden=false;};xhr.send();
 }
 // Management opens the normal authenticated router UI, never an open write API.
 el("manage").href="https://"+window.location.hostname+"/services/yachtsense-link-emulator";
 el("search").addEventListener("input",showDevices);poll();window.setInterval(poll,5000);
}());

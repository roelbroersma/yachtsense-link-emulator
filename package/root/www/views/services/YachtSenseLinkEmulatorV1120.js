/* YachtSense Link Emulator UI 1.1.2: observed status, explicit choices, no network
 * mutation by default. No inline CSS, remote assets or private VuCI widgets. */
const h = globalThis.Vue?.h;
const API = "/api/yachtsense-link-emulator-v1100";
const clone = (v) => JSON.parse(JSON.stringify(v));
const array = (v) => Array.isArray(v) ? v : [];
const defaults = () => ({enabled:false,mdns_enabled:true,web_enabled:true,raynet_mode:"auto",app_mode:"auto",axiom_interface:"br-lan",remote_interfaces:["br-lan"],discovery_mode:"auto",manage_ip:false,ipaddr:"198.18.0.1",prefix:21,remove_ip_on_stop:false,serial:"AF002A4",version:"V142.242.530",hostname:"yachtsense-main",instance:"yachtsense-main Settings",ttl:120,health_port:7777,log_level:"info",log_lines:40});
export function unwrap(value, predicate, depth=0) {
  if (!value || typeof value !== "object" || depth>8) return null;
  if (predicate(value)) return value;
  for (const key of ["data","result","response"]) { const out=unwrap(value[key],predicate,depth+1);if(out)return out; }
  return null;
}
const resultOf = (v) => unwrap(v,(x)=>typeof x.ok==="boolean");
function messageOf(error) { const data=error?.response?.data;const body=resultOf(data);const details=array(data?.errors).map(e=>e.error||e.message).filter(Boolean).join("; ");if(error?.response?.status===403)return details||"RutOS denied access (403). Sign in again after updating the package to refresh your permissions.";return body?.message||details||(error?.code==="ECONNABORTED"?"The request timed out. The previous status is no longer considered current.":error?.message)||"Request failed"; }
function stylesheet() { const id="ysle-1120-css";if(document.getElementById(id))return;const link=document.createElement("link");link.id=id;link.rel="stylesheet";link.href="/assets/yachtsense-link-emulator-v1120.css";document.head.appendChild(link); }
const modeLabel = (s) => ({auto:"Automatic",builtin:"Built-in relay",avahi:"Existing Avahi reflector",direct:"Direct discovery",disabled:"Relay disabled",conflict:"Reflector conflict"}[s] || "Not active");
export default {
  name:"YachtSenseLinkEmulatorV1100",
  data(){return {alive:false,loading:true,inFlight:false,timer:null,form:defaults(),saved:defaults(),payload:null,statusValid:false,statusError:"",busy:"",actionError:"",localAction:null,editor:"",help:false,advancedOpen:false,diagnosticsOpen:false,diagnostics:null,diagnosticLoading:false,diagnosticError:"",editRevision:0,writeEpoch:0,showAll:false};},
  mounted(){stylesheet();this.alive=true;this.poll();},
  beforeUnmount(){this.alive=false;if(this.timer)clearTimeout(this.timer);},
  methods:{
    isDirty(){return JSON.stringify(this.form)!==JSON.stringify(this.saved);},
    mark(key,value){this.form[key]=value;this.editRevision++;},
    pending(){const a=this.payload?.action;const local=this.localAction;if(local&&a?.id!==local.id&&Date.now()-local.at<65000)return true;return !!a&&["queued","applying"].includes(a.state);},
    async poll(){await this.refresh();if(this.alive)this.timer=setTimeout(()=>this.poll(),this.pending()?1500:5000);},
    async refresh(){
      if(this.inFlight||!this.alive)return;this.inFlight=true;const epoch=this.writeEpoch;
      try{const response=await this.$axios.get(`${API}/status`,{timeout:6500});if(!this.alive||epoch!==this.writeEpoch)return;
        const p=unwrap(response,(x)=>x.config&&x.status);if(!p)throw new Error(resultOf(response)?.message||"Unexpected backend response");
        const configError=array(p.status?.errors).find(e=>String(e).startsWith("cannot read configuration:"));if(configError)throw new Error(configError);
        const cfg={...defaults(),...p.config,remote_interfaces:array(p.config.remote_interfaces)};
        if(!this.isDirty()&&!this.busy)this.form=clone(cfg);this.saved=clone(cfg);this.payload=p;this.statusValid=p.snapshot_ok!==false;this.statusError=this.statusValid?"":"Interface status could not be read";
        if(this.localAction&&p.action?.id===this.localAction.id&&!this.pending())this.localAction=null;
      }catch(e){if(this.alive&&epoch===this.writeEpoch){this.statusValid=false;this.statusError=messageOf(e);}}
      finally{this.inFlight=false;this.loading=false;}
    },
    discard(){this.form=clone(this.saved);this.editRevision++;this.editor="";this.actionError="";},
    toggleRemote(name,checked){const s=new Set(array(this.form.remote_interfaces));checked?s.add(name):s.delete(name);this.mark("remote_interfaces",[...s]);},
    manageAddress(on){this.mark("manage_ip",on);if(on){this.mark("raynet_mode","manual");this.mark("axiom_interface",this.payload?.networks?.raynet||this.form.axiom_interface);}else this.mark("remove_ip_on_stop",false);},
    chooseAuto(){this.mark("raynet_mode","auto");this.mark("manage_ip",false);this.mark("remove_ip_on_stop",false);},
    async save(){
      if(this.busy||this.pending()||!this.statusValid)return;this.busy="save";this.actionError="";this.writeEpoch++;const revision=this.editRevision;
      const data={...clone(this.form),prefix:Number(this.form.prefix),ttl:Number(this.form.ttl),health_port:Number(this.form.health_port),log_lines:Number(this.form.log_lines)};
      try{const response=await this.$axios.post(`${API}/actions/save`,{data:{payload:JSON.stringify(data)}},{timeout:10000});const r=resultOf(response);if(r?.ok!==true)throw new Error(r?.message||"Save did not return a confirmed result");if(!this.alive)return;
        this.saved=clone(data);if(revision===this.editRevision)this.form=clone(data);this.localAction={id:r.action_id,at:Date.now()};this.editor="";
      }catch(e){if(this.alive)this.actionError=messageOf(e);}finally{this.busy="";if(this.alive)await this.refresh();}
    },
    async action(kind){
      if(this.busy||this.pending()||this.isDirty()||!this.statusValid)return;this.busy=kind;this.actionError="";this.writeEpoch++;
      try{const response=await this.$axios.post(`${API}/actions/${kind}`,{data:{}},{timeout:10000});const r=resultOf(response);if(r?.ok!==true)throw new Error(r?.message||"Service operation was not confirmed");if(!this.alive)return;this.localAction={id:r.action_id,at:Date.now()};
      }catch(e){if(this.alive)this.actionError=messageOf(e);}finally{this.busy="";if(this.alive)await this.refresh();}
    },
    async loadDiagnostics(){if(this.diagnosticLoading)return;this.diagnosticLoading=true;this.diagnosticError="";try{const r=await this.$axios.get(`${API}/diagnostics`,{timeout:14000});const p=unwrap(r,(x)=>x.diagnostics);if(!p)throw new Error(resultOf(r)?.message||"Diagnostics are unavailable");if(this.alive)this.diagnostics=p;}catch(e){if(this.alive)this.diagnosticError=messageOf(e);}finally{this.diagnosticLoading=false;}},
    overall(){
      if(this.loading)return {kind:"neutral",title:"Checking your system",detail:"Reading service state and existing network interfaces."};
      if(!this.statusValid)return {kind:"warn",title:"Current status is unavailable",detail:this.statusError};
      const p=this.payload,s=p.status,v=s.runtime||{},errors=array(s.errors);
      if(this.pending())return {kind:"wait",title:"Applying your changes",detail:p.action?.message||"Waiting for the service operation to finish."};
      if(p.action?.state==="failed")return {kind:"warn",title:"The last change needs attention",detail:p.action.message};
      if(!p.config.enabled&&!s.running)return {kind:"neutral",title:"YachtSense Link Emulator is off",detail:"Your existing network configuration is left unchanged."};
      if(!s.running)return {kind:"bad",title:"The service is not running",detail:errors[0]||"Open Diagnostics to see why startup failed."};
      if(s.stale)return {kind:"warn",title:"Service status is stale",detail:"A process is present, but its live status has stopped updating."};
      if(s.config_pending)return {kind:"warn",title:"Saved settings are not active yet",detail:"Restart the service under Diagnostics to apply them."};
      if(errors.length)return {kind:"warn",title:"Network choices need attention",detail:errors[0]};
      if(!s.mdns_active||!s.http_active)return {kind:"warn",title:"A core component is unavailable",detail:"YachtSense identity and HTTP health should normally both be enabled; see Advanced and Diagnostics."};
      if(v.engine==="disabled"||v.engine==="conflict")return {kind:"warn",title:"Cross-network discovery is unavailable",detail:v.reason||"Review your Discovery choice."};
      if(v.engine==="builtin"&&!s.relay_active)return {kind:"bad",title:"The built-in relay is not active",detail:"The requested relay did not reach a running state."};
      if(!s.axiom_detected)return {kind:"wait",title:"Ready — waiting for an Axiom",detail:"The local emulator is running. No current MFD service advertisement has been detected."};
      if(v.engine==="avahi")return {kind:"wait",title:"Axiom detected — Avahi selected",detail:"Avahi's process and configuration are detected; end-to-end forwarding is not independently verified."};
      return {kind:"ok",title:"YachtSense Link Emulator is ready",detail:s.app_detected?"Current MFD and app-discovery traffic have been seen; this does not verify a remote-control session.":"Axiom detected. Open the Raymarine app to check discovery from your phone or tablet."};
    },
    button(text,fn,kind="",disabled=false,attrs={}){return h("button",{type:"button",class:["ys-btn",kind],disabled,onClick:fn,...attrs},text);},
    badge(kind,text){return h("span",{class:["ys-state",kind]},[h("span",{"aria-hidden":"true"},kind==="ok"?"✓":kind==="bad"||kind==="warn"?"!":"○"),text]);},
    row(label,main,detail,kind,tag,edit=""){return h("div",{class:"ys-row",key:label},[h("div",{class:"ys-row-label"},label),h("div",{class:"ys-row-value"},[h("strong",this.statusValid?main:"Status unavailable"),this.statusValid&&detail?h("span",{class:"ys-note"},detail):null]),h("div",{class:"ys-row-end"},[this.badge(this.statusValid?kind:"neutral",this.statusValid?tag:"Unknown"),edit?this.button(this.editor===edit?"Done":"Change…",()=>{this.editor=this.editor===edit?"":edit;},"link",!!this.busy||!this.statusValid,{"aria-expanded":this.editor===edit}):null])]);},
    radio(group,value,label,detail,fn){return h("label",{class:"ys-choice",key:value},[h("input",{type:"radio",name:`ys-${group}`,checked:this.form[group]===value,disabled:!!this.busy||!this.statusValid,onChange:fn}),h("span",[h("strong",label),h("small",detail)])]);},
    toggle(key,label,detail,fn){return h("label",{class:"ys-choice"},[h("input",{type:"checkbox",checked:!!this.form[key],disabled:!!this.busy||!this.statusValid,onChange:(e)=>fn?fn(e.target.checked):this.mark(key,e.target.checked)}),h("span",[h("strong",label),h("small",detail)])]);},
    field(key,label,type="text",detail="",extra={}){return h("div",{class:"ys-field"},[h("label",{for:`ys-${key}`},label),h("input",{id:`ys-${key}`,type,value:this.form[key],disabled:!!this.busy||!this.statusValid,onInput:e=>this.mark(key,e.target.value),...extra}),detail?h("small",detail):null]);},
    netEditor(){
      const all=array(this.payload?.interfaces);const ed=this.editor;
      if(ed==="raynet")return h("div",{class:"ys-editor"},[
        this.radio("raynet_mode","auto","Automatic (recommended)",`Find the interface that already owns ${this.form.ipaddr}; do not add or remove addresses.`,()=>this.chooseAuto()),
        this.radio("raynet_mode","manual","Choose an interface","Keep this choice even after refreshing or restarting the router.",()=>this.mark("raynet_mode","manual")),
        this.form.raynet_mode==="manual"?h("label",{class:"ys-field"},[h("span","RayNet interface"),h("select",{value:this.form.axiom_interface,disabled:!!this.busy||!this.statusValid,onChange:e=>this.mark("axiom_interface",e.target.value)},[...all.map(i=>h("option",{value:i.name,key:i.name},`${i.name} — ${array(i.addresses).join(", ")||"No IPv4 address"}`)),!all.some(i=>i.name===this.form.axiom_interface)?h("option",{value:this.form.axiom_interface},`${this.form.axiom_interface} — unavailable`):null])]):null
      ]);
      if(ed==="app")return h("div",{class:"ys-editor"},[
        this.radio("app_mode","auto","Automatic (recommended)","Prefer br-lan with a private IPv4 address; never automatically choose a WAN/default-route interface.",()=>this.mark("app_mode","auto")),
        this.radio("app_mode","manual","Choose app network(s)","Choose where your phone or tablet connects; this may be the same interface as RayNet.",()=>this.mark("app_mode","manual")),
        this.form.app_mode==="manual"?h("div",[
          ...all.filter(i=>this.showAll||array(i.addresses).length||this.form.remote_interfaces.includes(i.name)).map(i=>h("label",{class:"ys-choice",key:i.name},[h("input",{type:"checkbox",checked:this.form.remote_interfaces.includes(i.name),disabled:!!this.busy||!this.statusValid,onChange:e=>this.toggleRemote(i.name,e.target.checked)}),h("span",[h("strong",i.name),h("small",array(i.addresses).join(", ")||"No IPv4 address — unavailable until configured")])])),
          this.button(this.showAll?"Hide interfaces without IPv4":"Show all interfaces",()=>{this.showAll=!this.showAll;},"link")
        ]):null
      ]);
      if(ed==="discovery")return h("div",{class:"ys-editor"},[
        this.radio("discovery_mode","auto","Automatic (recommended)","Use an eligible existing Avahi reflector; otherwise use the built-in relay. A same-interface setup needs no relay.",()=>this.mark("discovery_mode","auto")),
        this.radio("discovery_mode","builtin","Built-in relay","Use the emulator for Raymarine discovery. An overlapping Avahi reflector must first be disabled in its own settings.",()=>this.mark("discovery_mode","builtin")),
        this.radio("discovery_mode","avahi","Existing Avahi reflector",this.payload?.avahi?.eligible?"A matching process and configuration were found; forwarding still needs a client-side check.":`Not ready: ${this.payload?.avahi?.reason||"No reflector detected"}. No Avahi settings will be changed.`,()=>this.mark("discovery_mode","avahi")),
        this.radio("discovery_mode","disabled","No cross-network relay","Advanced: keep YachtSense identity and HTTP, but do not provide a discovery relay.",()=>this.mark("discovery_mode","disabled"))
      ]);
      return null;
    },
    diagnosticsView(){
      const d=this.diagnostics?.diagnostics,v=this.diagnostics?.status?.runtime,a=this.diagnostics?.avahi;
      return h("div",{class:"ys-details-body"},[
        h("div",{class:"ys-toolbar"},[this.button(this.diagnosticLoading?"Reading…":"Refresh diagnostics",()=>this.loadDiagnostics(),"secondary",this.diagnosticLoading),this.button(this.busy==="restart"?"Queuing…":"Restart emulator",()=>this.action("restart"),"secondary",!!this.busy||this.pending()||this.isDirty()||!this.saved.enabled)]),
        this.diagnosticError?h("p",{class:"ys-error",role:"alert"},this.diagnosticError):null,
        d?h("dl",{class:"ys-tech"},Object.entries({"Firmware":d.firmware||"Unknown","Package":this.diagnostics?.status?.package_version,"Executable":d.executable,"Configuration":d.config_dir,"Process":v?.pid||"Not running","Actual RayNet":v?.raynet||"Not running","Actual app networks":array(v?.apps).join(", ")||"None","Actual discovery":modeLabel(v?.engine),"Avahi":a?.reason||"Not detected","umdns":a?.umdns?"Process detected; not assumed to be a reflector":"Not detected","mDNS publisher":this.diagnostics?.status?.mdns_active?"Active":"Inactive","HTTP health listener":this.diagnostics?.status?.http_active?"Active":"Inactive","Relayed queries/responses":`${v?.queries||0} / ${v?.responses||0}`}).flatMap(([k,val])=>[h("dt",{key:`k-${k}`},k),h("dd",{key:`v-${k}`},String(val??"Unknown"))])):h("p",{class:"ys-note"},"Technical details and logs are read only when you open or refresh Diagnostics."),
        d?h("div",[h("h4","UDP 5353 listeners"),h("pre",{class:"ys-log"},array(d.listeners).join("\n")||"No listeners reported"),h("h4","Last service operation"),h("pre",{class:"ys-log"},d.action_log||"No operation output"),h("h4","Logs"),h("pre",{class:"ys-log"},array(d.logs).join("\n")||"No log entries"),h("p",{class:"ys-note"},d.discovery_note)]):null
      ]);
    }
  },
  render(){
    if(!h)return null;const p=this.payload||{},s=p.status||{},v=s.runtime||{},n=p.networks||{},a=p.avahi||{},c=p.config||this.saved;const summary=this.overall();const dirty=this.isDirty();const disabled=!!this.busy||this.pending()||!this.statusValid;
    const actualRaynet=s.running?v.raynet:n.raynet;const actualApps=s.running?array(v.apps):array(n.apps);const iface=array(p.interfaces).find(i=>i.name===actualRaynet);const addresses=array(iface?.addresses);const hasRaynet=addresses.some(ip=>ip.split("/")[0]===c.ipaddr);const actualEngine=s.running?v.engine:s.configured_engine;
    const draftNote=dirty?"Unsaved changes below; statuses still describe the running service.":"Status reflects the running service, not just the selected settings.";
    return h("main",{class:"ys-page"},[
      h("header",{class:"ys-header"},[h("div",[h("p",{class:"ys-eyebrow"},"RAYMARINE · NETWORK DISCOVERY"),h("h2","YachtSense Link"),h("p",{class:"ys-subtitle"},"Your existing network. One clear view.")]),h("div",{class:"ys-power"},[h("span",this.statusValid?(c.enabled?"On":"Off"):"Unknown"),this.button("",()=>this.action(c.enabled?"stop":"start"),`switch ${this.statusValid&&c.enabled?"on":""}`,disabled||dirty||this.loading||!this.statusValid,{role:"switch","aria-checked":this.statusValid&&!!c.enabled,"aria-label":"Enable YachtSense Link Emulator",title:dirty?"Save or cancel changes before switching":"Enable or disable the saved configuration"})])]),
      h("section",{class:"ys-card"},[
        h("div",{class:"ys-hero","aria-live":"polite"},[h("div",{class:["ys-hero-icon",summary.kind],"aria-hidden":"true"},summary.kind==="ok"?"✓":summary.kind==="bad"||summary.kind==="warn"?"!":"○"),h("div",[h("h3",summary.title),h("p",summary.detail)])]),
        h("div",{class:"ys-status"},[
          this.row("Service",s.running?"Running":"Not running",s.config_pending?"Saved configuration differs from the running process":draftNote,s.running&&!s.stale?"ok":"neutral",s.running&&!s.stale?"Active":"Inactive"),
          this.row("RayNet",actualRaynet?`${actualRaynet} · ${s.running?v.cidr:n.cidr}`:`Looking for ${c.ipaddr}`,`${this.form.raynet_mode==="auto"?"Automatic detection":"Manual selection"}${this.form.manage_ip?" · Address management explicitly enabled":" · Existing addresses only"}`,hasRaynet?"ok":"warn",hasRaynet?"Detected":"Missing","raynet"),
          this.editor==="raynet"?this.netEditor():null,
          this.row("Axiom / MFD",s.axiom_detected?`${v.axiom?.ip} · ${v.axiom?.name||"MFD"}`:v.axiom?.ip?`${v.axiom.ip} · last seen earlier`:"Not detected yet",s.axiom_detected?"Current service advertisement observed":"Waiting for current Raymarine discovery traffic",s.axiom_detected?"ok":"neutral",s.axiom_detected?"Detected":"Waiting"),
          this.row("App network",actualApps.join(", ")||"Choose a network",s.app_detected?`Discovery query observed from ${v.app?.ip}`:`${this.form.app_mode==="auto"?"Automatic choice":"Manual choice"} · No recent app-discovery query observed`,actualApps.length?"ok":"warn",actualApps.length?"Selected":"Choose","app"),
          this.editor==="app"?this.netEditor():null,
          this.row("Discovery",modeLabel(actualEngine),s.running?(v.reason||""):(s.configured_reason||""),s.running&&actualEngine==="builtin"&&s.relay_active||s.running&&actualEngine==="direct"?"ok":actualEngine==="avahi"?"warn":"neutral",actualEngine==="avahi"?"Not verified":s.relay_active?"Active":actualEngine==="direct"&&s.running?"Direct":"Inactive","discovery"),
          this.editor==="discovery"?this.netEditor():null
        ]),
        h("div",{class:"ys-card-foot"},[h("span",`Choice: ${modeLabel(this.form.discovery_mode)}${dirty?" · not saved":""}`),this.button(this.help?"Hide explanation":"How does discovery work?",()=>{this.help=!this.help;},"link",false,{"aria-expanded":this.help})]),
        this.help?h("div",{class:"ys-help"},[h("strong","Publisher and relay are different roles."),h("p","The built-in daemon always supplies the YachtSense identity and HTTP health response. Avahi can replace only the cross-network mDNS relay, not the emulator itself."),h("p",a.reason||"No Avahi process detected."),h("p","A running Avahi responder or an occupied UDP 5353 port does not by itself mean there is a reflector. Green checks mean the named component was observed; they do not confirm internet access or a working remote-control session.")]):null
      ]),
      this.actionError?h("div",{class:"ys-error ys-banner",role:"alert"},this.actionError):null,
      h("details",{class:"ys-details",open:this.advancedOpen,onToggle:e=>{this.advancedOpen=e.target.open;}},[h("summary","Advanced"),h("div",{class:"ys-details-body"},[
        h("p",{class:"ys-note"},"Normally no changes are needed here. Address management changes the selected interface only when explicitly enabled."),
        this.toggle("manage_ip","Manage the RayNet address","Off means observe only; an existing address is never claimed or removed.",(on)=>this.manageAddress(on)),
        this.form.manage_ip?this.toggle("remove_ip_on_stop","Remove an address added by this package on stop","An address that already existed is never removed."):null,
        h("div",{class:"ys-form-grid"},[this.field("ipaddr","RayNet address to find","text","Normally 198.18.0.1"),this.field("prefix","Prefix for explicitly added addresses","number","Automatic detection uses the prefix already present.",{min:0,max:32}),this.field("serial","YachtSense serial"),this.field("version","Advertised firmware string"),this.field("hostname","mDNS hostname"),this.field("instance","Service instance"),this.field("health_port","HTTP health port","number","This does not change the advertised SRV identity port 80.",{min:1,max:65535}),this.field("ttl","Advertisement TTL (seconds)","number","",{min:1,max:86400}),this.field("log_lines","Lines in Diagnostics","number","",{min:5,max:200}),h("label",{class:"ys-field"},[h("span","Log detail"),h("select",{value:this.form.log_level,disabled:!!this.busy||!this.statusValid,onChange:e=>this.mark("log_level",e.target.value)},[h("option",{value:"info"},"Info"),h("option",{value:"debug"},"Debug")])])]),
        h("h4","Individual components"),this.toggle("mdns_enabled","YachtSense identity publisher","Normally on; disabling it may prevent Axiom from finding the emulator."),this.toggle("web_enabled","HTTP health response","Normally on; this is a local liveness response, not an internet connectivity test.")
      ])]),
      h("details",{class:"ys-details",open:this.diagnosticsOpen,onToggle:e=>{this.diagnosticsOpen=e.target.open;if(e.target.open)this.loadDiagnostics();}},[h("summary","Diagnostics & logs"),this.diagnosticsOpen?this.diagnosticsView():null]),
      h("footer",{class:"ys-footer"},`Package ${s.package_version||"1.1.2"} · UI 1.1.2 · Settings stored on this router`),
      dirty?h("div",{class:"ys-savebar",role:"region","aria-label":"Unsaved settings"},[h("span","Unsaved changes"),h("div",[this.button("Cancel",()=>this.discard(),"secondary",!!this.busy),this.button(this.busy==="save"?"Saving…":"Save changes",()=>this.save(),"primary",disabled)])]):null
    ]);
  }
};

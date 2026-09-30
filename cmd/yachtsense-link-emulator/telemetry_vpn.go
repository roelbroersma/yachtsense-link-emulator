package main

// Read-only router telemetry. Missing observations are never invented.
import("regexp";"sort";"strconv";"strings";"time")
func rmsState(v any)string{
 m:=obj(v);for _,k:=range []string{"connection_status","connection_state","status"}{if s:=statusText(m[k]);s!="unknown"{return s}}
 // librms.h defines RMS_CONNECTED=0 and RMS_DISCONNECTED=1.
 if n,ok:=number(m["connection_status"]);ok{if n==0{return "connected"};if n==1{return "disconnected"}}
 if b,ok:=truth(m["connected"]);ok{if b{return "connected"};return "disconnected"}
 for _,k:=range []string{"rms","data"}{if x:=obj(m[k]);x!=nil{if s:=rmsState(x);s!="unknown"{return s}}};return "unknown"
}
var ikeHeaderRE=regexp.MustCompile(`^(\S.*?):\s+#\d+,\s*([A-Z_]+),\s*IKEv[12]`)
var ikeUptimeRE=regexp.MustCompile(`established\s+([0-9]+)s\s+ago`)
var ikeBytesRE=regexp.MustCompile(`^\s*(in|out)\s+[^,]+,\s*([0-9]+)\s+bytes`)
func swanVPN(data []byte)[]VPNView{
 result:=[]VPNView{};idx:=-1
 for _,line:=range strings.Split(string(data),"\n"){
  if m:=ikeHeaderRE.FindStringSubmatch(line);m!=nil{result=append(result,VPNView{Name:text(m[1]),Kind:"IPsec",State:"connecting"});idx=len(result)-1;if m[2]!="ESTABLISHED"{result[idx].Note=m[2]};continue}
  if idx<0{continue};v:=&result[idx]
  if m:=ikeUptimeRE.FindStringSubmatch(line);m!=nil{n,_:=strconv.ParseFloat(m[1],64);v.Uptime=&n}
  if strings.Contains(line,"INSTALLED")&&(strings.Contains(line,"TUNNEL")||strings.Contains(line,"TRANSPORT")){v.Children++;v.State="connected"}
  if m:=ikeBytesRE.FindStringSubmatch(line);m!=nil{n,_:=strconv.ParseFloat(m[2],64);if m[1]=="in"{if v.RX==nil{v.RX=new(float64)};*v.RX+=n}else{if v.TX==nil{v.TX=new(float64)};*v.TX+=n}}
 };return result
}
func vpnAPI(v any,kind string)[]VPNView{
 out:=[]VPNView{};for _,m:=range rows(v){name:=first(m,"name","id","instance","connection");if name==""{continue};s:="unknown";for _,k:=range []string{"connection_status","state","status"}{if v:=statusText(m[k]);v!="unknown"{s=v;break}};if b,ok:=truth(m["connected"]);ok{if b{s="connected"}else{s="disconnected"}};out=append(out,VPNView{Name:name,Kind:kind,State:s,Uptime:num(m,"uptime","connected_seconds"),RX:num(m,"bytes_in","rx_bytes"),TX:num(m,"bytes_out","tx_bytes")});if len(out)>=64{break}};return out
}
func wgVPN(handshakes,transfers []byte,now time.Time)[]VPNView{
 out:=[]VPNView{};byIf:=map[string]*VPNView{}
 for _,line:=range strings.Split(string(handshakes),"\n"){f:=strings.Fields(line);if len(f)!=3||!safeInterface(f[0]){continue};epoch,e:=strconv.ParseInt(f[2],10,64);if e!=nil{continue};v:=byIf[f[0]];if v==nil{v=&VPNView{Name:f[0],Kind:"WireGuard",State:"unknown",Note:"WireGuard has no connected-session timer; recent handshake is shown instead."};byIf[f[0]]=v};if epoch>0{age:=now.Sub(time.Unix(epoch,0)).Seconds();if age>=0&&age<180{v.State="recent_handshake";v.HandshakeAge=&age}}}
 for _,line:=range strings.Split(string(transfers),"\n"){f:=strings.Fields(line);if len(f)!=4{continue};if v:=byIf[f[0]];v!=nil{rx,ok1:=number(f[2]);tx,ok2:=number(f[3]);if ok1&&ok2{if v.RX==nil{v.RX=new(float64);v.TX=new(float64)};*v.RX+=rx;*v.TX+=tx}}}
 for _,v:=range byIf{out=append(out,*v)};sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});return out
}
func vxlanView(v any)[]VXLANView{
 out:=[]VXLANView{};for _,lv:=range list(v){m:=obj(lv);info:=obj(m["linkinfo"]);if first(info,"info_kind")!="vxlan"{continue};d:=obj(info["info_data"]);up:=false;for _,f:=range list(m["flags"]){if text(f)=="UP"{up=true}};stats:=obj(m["stats64"]);if stats==nil{stats=obj(m["stats"])};out=append(out,VXLANView{Name:first(m,"ifname"),Up:up,Operstate:first(m,"operstate"),VNI:num(d,"id"),Remote:first(d,"remote","group"),MTU:num(m,"mtu"),RX:num(obj(stats["rx"]),"bytes"),TX:num(obj(stats["tx"]),"bytes"),Note:"UP is the local interface state, not proof that the remote VTEP is reachable."})};return out
}

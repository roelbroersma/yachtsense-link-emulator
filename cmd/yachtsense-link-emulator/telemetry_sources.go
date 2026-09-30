package main

// Read-only router telemetry. Missing observations are never invented.
import("context";"encoding/json";"io";"math";"os";"sort";"strconv";"strings";"sync";"time")
type rawSource struct{data []byte;err error}
type probe struct{name,program string;args []string}
var routerProbes=[]probe{
 {"interfaces","ubus",[]string{"call","network.interface","dump"}},
 {"wireless","ubus",[]string{"call","network.wireless","status"}},
 {"failover","mwan3",[]string{"status"}},
 {"profile","uci",[]string{"-q","get","profiles.general.profile"}},
 {"gps","api",[]string{"get","/gps/position/status"}},
 {"gps_fix","api",[]string{"get","/gps/status"}},
 {"geofences","api",[]string{"get","/gps/geofencing/config"}},
 {"rms","ubus",[]string{"call","rms","get_status"}},
 {"rms_api","api",[]string{"get","/rms/status"}},
 {"ipsec","swanctl",[]string{"--list-sas"}},
 {"ipsec_api","api",[]string{"get","/ipsec/status"}},
 {"openvpn","api",[]string{"get","/openvpn/status"}},
 {"wireguard","wg",[]string{"show","all","latest-handshakes"}},
 {"wg_transfer","wg",[]string{"show","all","transfer"}},
 {"vxlan","ip",[]string{"-d","-s","-j","link","show","type","vxlan"}},
 {"neighbors","ip",[]string{"-j","-4","neigh","show"}},
 {"routes","ip",[]string{"-j","-4","route","show","default"}},
 {"mobile","gsmctl",[]string{"-E"}},
 {"signal","gsmctl",[]string{"-q"}},
 {"operator","gsmctl",[]string{"-o"}},
 {"sim","gsmctl",[]string{"-z"}},
 {"fdb","bridge",[]string{"-j","fdb","show"}},
 {"logs","logread",[]string{"-e",serviceName}},
}
func readSmall(path string)[]byte{f,err:=os.Open(path);if err!=nil{return nil};defer f.Close();b,err:=io.ReadAll(io.LimitReader(f,131073));if err!=nil||len(b)>131072{return nil};return b}
func probeAll(ctx context.Context)map[string]rawSource{
 result:=map[string]rawSource{};var mu sync.Mutex;var wg sync.WaitGroup;sem:=make(chan struct{},4)
 // Modem commands are serialized; modem control must not be flooded.
 var modem sync.Mutex
 for _,p:=range routerProbes{wg.Add(1);go func(p probe){defer wg.Done();select{case sem<-struct{}{}:case<-ctx.Done():return};defer func(){<-sem}();if ctx.Err()!=nil{return};if p.program=="gsmctl"{modem.Lock();defer modem.Unlock()};b,err:=runCommand(2500*time.Millisecond,nil,findProgram("/usr/local/sbin/"+p.program,"/usr/local/usr/sbin/"+p.program,p.program),p.args...);mu.Lock();result[p.name]=rawSource{b,err};mu.Unlock()}(p)}
 wg.Wait();return result
}
func decodeSource(raw rawSource)any{
 if raw.err!=nil||len(raw.data)>131072{return nil};var v any;dec:=json.NewDecoder(strings.NewReader(string(raw.data)));dec.UseNumber();if dec.Decode(&v)!=nil{return nil}
 m:=obj(v);if code,ok:=number(m["http_code"]);ok{if code!=200{return nil};m=obj(m["http_body"]);if m==nil{return nil};v=m}
 if success,ok:=m["success"].(bool);ok{if !success{return nil};return m["data"]};return v
}
func obj(v any)map[string]any{m,_:=v.(map[string]any);return m}
func list(v any)[]any{a,_:=v.([]any);return a}
func text(v any)string{
 var s string;switch x:=v.(type){case string:s=x;case json.Number:s=x.String();default:return ""}
 s=strings.TrimSpace(s);s=strings.Map(func(r rune)rune{if r<32||r==127{return -1};return r},s);if len(s)>160{s=s[:160]};return s
}
func first(m map[string]any,keys ...string)string{for _,k:=range keys{if s:=text(m[k]);s!=""{return s}};return ""}
func number(v any)(float64,bool){var f float64;var e error;switch n:=v.(type){case json.Number:f,e=n.Float64();case float64:f=n;case string:f,e=strconv.ParseFloat(strings.TrimSpace(n),64);default:return 0,false};return f,e==nil&&!math.IsNaN(f)&&!math.IsInf(f,0)}
func num(m map[string]any,keys ...string)*float64{for _,k:=range keys{if n,ok:=number(m[k]);ok{return &n}};return nil}
func truth(v any)(bool,bool){if b,ok:=v.(bool);ok{return b,true};switch strings.ToLower(text(v)){case "1","true","yes","on","enabled":return true,true;case "0","false","no","off","disabled":return false,true};return false,false}
func statusText(v any)string{switch strings.ToLower(text(v)){case "connected","established","up","online","installed":return "connected";case "disconnected","down","offline","disabled","not connected":return "disconnected";case "connecting","reconnecting","connecting...":return "connecting"};return "unknown"}
func rows(v any)[]map[string]any{
 if a,ok:=v.([]any);ok{out:=[]map[string]any{};for _,x:=range a{if m:=obj(x);m!=nil{out=append(out,m)}};return out}
 m:=obj(v);if m==nil{return nil}
 for _,k:=range []string{"instances","connections","profiles","position","geofences","results"}{if a,ok:=m[k].([]any);ok{return rows(a)}}
 // Single objects retain their fields. Keyed maps are expanded deterministically.
 for _,k:=range []string{"id","name","latitude","lat","status","state","connection_status"}{if _,ok:=m[k];ok{return []map[string]any{m}}}
 keys:=[]string{};for k,x:=range m{if obj(x)!=nil{keys=append(keys,k)}};sort.Strings(keys);if len(keys)==0{return []map[string]any{m}}
 out:=[]map[string]any{};for _,k:=range keys{x:=obj(m[k]);n:=map[string]any{"id":k};for a,b:=range x{n[a]=b};out=append(out,n)};return out
}
// Parse system configuration without running shell text or publishing secrets.
type uciSection struct{typ,name string;values map[string][]string}
func uciSections(data []byte)[]uciSection{
 out:=[]uciSection{};for _,line:=range strings.Split(string(data),"\n"){w,e:=words(line);if e!=nil||len(w)==0{continue};if w[0]=="config"&&len(w)>=2{n:="";if len(w)>2{n=w[2]};out=append(out,uciSection{w[1],n,map[string][]string{}})};if len(out)>0&&len(w)==3&&(w[0]=="option"||w[0]=="list"){m:=out[len(out)-1].values;if w[0]=="option"{m[w[1]]=[]string{w[2]}}else{m[w[1]]=append(m[w[1]],w[2])}}};return out
}
func uv(s uciSection,k string)string{a:=s.values[k];if len(a)==0{return ""};return a[len(a)-1]}
func systemConfig(name string)[]byte{b:=readSmall("/etc/config/"+name);if len(b)==0{b=readSmall("/usr/local/etc/config/"+name)};return b}

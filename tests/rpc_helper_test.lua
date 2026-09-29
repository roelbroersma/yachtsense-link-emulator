-- Test the shipped finite RPC helper without executing privileged operations.
local script="package/root/usr/libexec/rpcd/yachtsense-link-emulator"
local output,input,raw,response,command,have_binary
package.preload["luci.jsonc"]=function()
  return {parse=function(v) if v==raw then return input end;return response end,
          stringify=function(v) output.value=v;return "fixture-result" end}
end
local original={open=io.open,popen=io.popen,read=io.read,write=io.write}
io.read=function() return raw end
io.write=function(...)
  for i=1,select("#",...) do output.text=output.text..tostring(select(i,...)) end
end
io.open=function(path)
  if path=="/etc/opkg.conf" then
    return {lines=function() local done=false;return function() if done then return nil end;done=true;return "dest root /usr/local" end end,close=function() end}
  end
  if have_binary and path=="/usr/local/usr/sbin/yachtsense-link-emulator" then return {close=function() end} end
end
io.popen=function(cmd)
  command=cmd;return {read=function() return "backend-output" end,close=function() end}
end
local function run(method, args, options)
  options=options or {};output={text=""};input=args;raw=options.raw or "request-envelope";response=options.response or {ok=true}
  command=nil;have_binary=not options.missing;arg={options.op or "call",method}
  assert(loadfile(script))()
  return output,command
end
local count=0
local function check(name,fn) fn();count=count+1;print("PASS "..name) end
check("list exposes only six supported methods",function()
  local r,cmd=run(nil,nil,{op="list"});assert(r.text:find('"status":{}',1,true));assert(r.text:find('"save":{"payload":""}',1,true));assert(not cmd)
end)
check("status resolves package root",function() local r,cmd=run("status",{});assert(cmd:find("/usr/local/usr/sbin/",1,true));assert(r.text:find("backend-output",1,true)) end)
check("diagnostics uses the longer deadline",function() local _,cmd=run("diagnostics",{});assert(cmd:find("timeout 12",1,true)) end)
check("unknown method is rejected",function() local r,cmd=run("execute",{});assert(r.value.ok==false and cmd==nil) end)
check("arbitrary command argument is rejected",function() local r,cmd=run("start",{command="reboot"});assert(r.value.ok==false and cmd==nil) end)
check("payload is forbidden on status",function() local r,cmd=run("status",{payload="{}"});assert(r.value.ok==false and cmd==nil) end)
check("save requires bounded string payload",function() local r=run("save",{payload=string.rep("a",16385)});assert(r.value.ok==false) end)
check("NUL is rejected before the shell",function() local r,cmd=run("save",{payload="a\0b"});assert(r.value.ok==false and cmd==nil) end)
check("payload shell syntax remains quoted data",function()
  local r,cmd=run("save",{payload=[[{"hostname":"literal ' $(echo test)"}]]})
  assert(cmd:find([[literal '\'' $(echo test)]],1,true));assert(r.text:find("backend-output",1,true))
end)
check("root-only backend is required",function() local r,cmd=run("status",{},{missing=true});assert(r.value.ok==false and cmd==nil) end)
check("oversized RPC envelope is rejected",function() local r,cmd=run("save",{payload="{}"},{raw=string.rep("x",131073)});assert(r.value.ok==false and cmd==nil) end)
check("invalid backend response is rejected",function() local r=run("status",{},{response={message="missing ok"}});assert(r.value.ok==false) end)
check("optional session field cannot inject commands",function() local _,cmd=run("status",{ubus_rpc_session="$(reboot)"});assert(cmd and not cmd:find("reboot",1,true)) end)
for k,v in pairs(original) do io[k]=v end
print(count.." finite RPC helper tests passed (mocked I/O, no privileged commands executed)")

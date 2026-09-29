-- Exercise the real API adapter with a mocked authenticated RutOS/ubus contract.
local response, connect_ok, throw_call, ubus_code = {ok=true}, true, false, nil
local closed, last_object, last_method, last_args, save_option = 0, nil, nil, nil, nil
package.preload["api/FunctionService"] = function()
  local F={}
  function F:new()
    local s={arguments={data={}}}
    function s:ResponseOK(v) return v end
    function s:action(name, fn)
      return {option=function() save_option={}; return save_option end}
    end
    return s
  end
  return F
end
package.preload["ubus"] = function()
  return {connect=function()
    if not connect_ok then return nil end
    return {
      call=function(_, object, method, args)
        last_object,last_method,last_args=object,method,args
        if throw_call then error("fixture transport failure") end
        return response,ubus_code
      end,
      close=function() closed=closed+1 end
    }
  end}
end
local s=assert(loadfile("package/root/usr/lib/lua/api/services/yachtsense_link_emulator_v1100.lua"))()
local count=0
local function check(name, fn)
  response,connect_ok,throw_call,ubus_code={ok=true},true,false,nil
  fn();count=count+1;print("PASS "..name)
end
check("status uses only the finite RPC object",function()
  assert(s:GET_TYPE_status().ok)
  assert(last_object=="yachtsense-link-emulator" and last_method=="status" and next(last_args)==nil)
end)
check("diagnostics uses its own RPC method",function() assert(s:GET_TYPE_diagnostics().ok);assert(last_method=="diagnostics") end)
check("save passes literal data without shell execution",function()
  local payload=[[{"hostname":"literal ' $(echo test)"}]]
  s.arguments.data.payload=payload
  assert(s:SaveAction().ok and last_args.payload==payload and last_method=="save")
end)
check("invalid save payload fails before ubus",function()
  local before=closed;s.arguments.data.payload={};assert(not s:SaveAction().ok);assert(closed==before)
end)
check("oversized save is refused",function() s.arguments.data.payload=string.rep("x",16385);assert(not s:SaveAction().ok) end)
check("start uses the start method",function() assert(s:StartAction().ok and last_method=="start") end)
check("stop uses the stop method",function() assert(s:StopAction().ok and last_method=="stop") end)
check("restart uses the restart method",function() assert(s:RestartAction().ok and last_method=="restart") end)
check("missing connection does not become success",function() connect_ok=false;assert(not s:GET_TYPE_status().ok) end)
check("missing object retains ubus code 4",function() response=nil;ubus_code=4;local r=s:GET_TYPE_status();assert(not r.ok and r.message:find("code 4",1,true)) end)
check("denied access retains ubus code 6",function() response=nil;ubus_code=6;local r=s:GET_TYPE_status();assert(not r.ok and r.message:find("code 6",1,true)) end)
check("transport exceptions still close the connection",function() local before=closed;throw_call=true;assert(not s:GET_TYPE_status().ok);assert(closed==before+1) end)
check("invalid result is not accepted",function() response={message="no status"};assert(not s:GET_TYPE_status().ok) end)
check("payload validator enforces its boundary",function() assert(save_option:validate("{}"));assert(not save_option:validate("x"));assert(not save_option:validate({}));assert(not save_option:validate(string.rep("x",16385))) end)
print(count.." Lua adapter tests passed (mocked RutOS API and ubus)")

-- Authenticated RutOS API adapter: use a finite rpcd bridge, not a process
-- inheriting the unprivileged uhttpd identity. Keep configuration root-only.
local FunctionService = require("api/FunctionService")
local ubus = require("ubus")
local Service = FunctionService:new()
local RPC_OBJECT = "yachtsense-link-emulator"

local function invoke(method, payload)
  local args = {}
  if method == "save" then
    if type(payload) ~= "string" or #payload < 2 or #payload > 16384 then
      return {ok=false, message="Save requires a JSON payload of 2 to 16384 bytes"}
    end
    args.payload = payload
  end
  local conn = ubus.connect()
  if not conn then return {ok=false, message="Could not connect to the local RutOS bus"} end
  local success, result, code = pcall(conn.call, conn, RPC_OBJECT, method, args)
  pcall(conn.close, conn)
  if not success then
    return {ok=false, message="YachtSense RPC bridge error: " .. tostring(result)}
  end
  if type(result) ~= "table" or type(result.ok) ~= "boolean" then
    return {ok=false, message="YachtSense RPC bridge is unavailable or access was denied (ubus code " .. tostring(code or "unknown") .. "); reload rpcd and its ACLs"}
  end
  return result
end
local function respond(self, method, payload)
  local success, result = pcall(invoke, method, payload)
  if not success then result={ok=false, message="YachtSense API error: " .. tostring(result)} end
  return self:ResponseOK(result)
end
function Service:GET_TYPE_status() return respond(self, "status") end
function Service:GET_TYPE_diagnostics() return respond(self, "diagnostics") end
function Service:SaveAction()
  local data = self.arguments and self.arguments.data or {}
  return respond(self, "save", data.payload)
end
local save = Service:action("save", Service.SaveAction)
local payload = save:option("payload")
payload.require = true
payload.maxlength = 16384
function payload:validate(value) return type(value)=="string" and #value>=2 and #value<=16384 end
function Service:StartAction() return respond(self, "start") end
function Service:StopAction() return respond(self, "stop") end
function Service:RestartAction() return respond(self, "restart") end
Service:action("start", Service.StartAction)
Service:action("stop", Service.StopAction)
Service:action("restart", Service.RestartAction)
return Service

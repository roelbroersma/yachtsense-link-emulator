-- Thin authenticated RutOS API adapter. All validation, configuration writes,
-- runtime detection and bounded service actions live in the static executable.
local FunctionService = require("api/FunctionService")
local json = require("luci.jsonc")
local Service = FunctionService:new()

local function quote(value)
  return "'" .. tostring(value or ""):gsub("'", "'\\''") .. "'"
end

local function executable()
  local root = ""
  local f = io.open("/etc/opkg.conf", "r")
  if f then
    for line in f:lines() do
      local path = line:match("^%s*dest%s+root%s+(%S+)")
      if path then root = path:gsub("/+$", ""); break end
    end
    f:close()
  end
  for _, path in ipairs({root .. "/usr/sbin/yachtsense-link-emulator", "/usr/local/usr/sbin/yachtsense-link-emulator", "/usr/sbin/yachtsense-link-emulator"}) do
    local file = io.open(path, "rb")
    if file then file:close(); return path end
  end
  return nil
end

local function invoke(method, payload)
  local program = executable()
  if not program then return {ok=false, message="YachtSense executable was not found in the package root"} end
  local timeout = method == "diagnostics" and 12 or 8
  local command = "printf '%s' " .. quote(payload or "") .. " | timeout " .. timeout .. " " .. quote(program) .. " --api " .. quote(method) .. " 2>/dev/null"
  local pipe = io.popen(command, "r")
  if not pipe then return {ok=false, message="Could not launch YachtSense backend"} end
  local text = pipe:read("*a") or ""
  pipe:close()
  local result = json.parse(text)
  if type(result) ~= "table" or type(result.ok) ~= "boolean" then
    return {ok=false, message="YachtSense backend timed out or returned invalid data; check Diagnostics or CLI --api status"}
  end
  return result
end

local function respond(self, method, payload)
  local success, result = pcall(invoke, method, payload)
  if not success then result = {ok=false, message="YachtSense API error: " .. tostring(result)} end
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
function payload:validate(value) return type(value) == "string" and #value >= 2 and #value <= 16384 end
function Service:StartAction() return respond(self, "start") end
function Service:StopAction() return respond(self, "stop") end
function Service:RestartAction() return respond(self, "restart") end
Service:action("start", Service.StartAction)
Service:action("stop", Service.StopAction)
Service:action("restart", Service.RestartAction)
return Service

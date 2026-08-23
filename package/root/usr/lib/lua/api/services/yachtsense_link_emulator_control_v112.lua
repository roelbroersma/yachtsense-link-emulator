-- YachtSense Link Emulator v1.0.12 service controls.
-- Service transitions are detached from the API request so VuCI cannot remain
-- blocked by rc.common/procd. Each shell action is bounded with BusyBox timeout.

local FunctionService = require("api/FunctionService")
local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local MANAGED_IP_FILE = "/var/run/yachtsense-link-emulator.managed-ip"
local ACTION_LOG = "/tmp/yachtsense-link-emulator-action.log"

local function trim(value)
	if value == nil then return "" end
	return tostring(value):match("^%s*(.-)%s*$") or ""
end

local function shell_quote(value)
	return "'" .. tostring(value or ""):gsub("'", "'\\''") .. "'"
end

local function command_output(command)
	local pipe = io.popen(command .. " 2>/dev/null")
	if not pipe then return "" end
	local output = pipe:read("*a") or ""
	pipe:close()
	return output:gsub("%s+$", "")
end

local function command_ok(command)
	local pipe = io.popen("(" .. command .. ") >/dev/null 2>&1; printf '%s' $?")
	if not pipe then return false end
	local status = pipe:read("*a") or "1"
	pipe:close()
	return tonumber(status) == 0
end

local function package_prefix()
	local root = command_output("awk '$1 == \"dest\" && $2 == \"root\" { print $3; exit }' /etc/opkg.conf")
	root = trim(root):gsub("/+$", "")
	if root == "" or root == "/" then return "" end
	return root
end

local PREFIX = package_prefix()
local PACKAGE_UCI_DIR = PREFIX .. "/etc/config"
local UCI_DIR = command_ok("test -r " .. shell_quote(PACKAGE_UCI_DIR .. "/" .. CONFIG)) and PACKAGE_UCI_DIR or "/etc/config"
local UCI = "uci -c " .. shell_quote(UCI_DIR)
local INIT = PREFIX .. "/etc/init.d/yachtsense-link-emulator"
local PROG = PREFIX .. "/usr/sbin/yachtsense-link-emulator"

local function uci_get(option, fallback)
	local key = CONFIG .. "." .. SECTION .. "." .. option
	local value = command_output(UCI .. " -q get " .. shell_quote(key))
	return value ~= "" and value or fallback
end

local function boolean_value(value, fallback)
	if value == true or value == 1 or value == "1" then return true end
	if value == false or value == 0 or value == "0" then return false end
	if value == nil then return fallback end
	local normalized = trim(value):lower()
	if normalized == "true" or normalized == "yes" or normalized == "on" or normalized == "enabled" then return true end
	if normalized == "false" or normalized == "no" or normalized == "off" or normalized == "disabled" then return false end
	return fallback
end

local function interface_exists(name)
	name = trim(name)
	if #name < 1 or #name > 64 or not name:match("^[%w%._:@%-]+$") then return false end
	local file = io.open("/sys/class/net/" .. name .. "/ifindex", "r")
	if not file then return false end
	file:close()
	return true
end

local function interface_has_cidr(name, wanted)
	if not interface_exists(name) then return false end
	local output = command_output("ip -o -4 addr show dev " .. shell_quote(name))
	for address in output:gmatch("inet%s+([%d%.]+/%d+)") do
		if address == wanted then return true end
	end
	return false
end

local function set_enabled(enabled)
	local assignment = CONFIG .. "." .. SECTION .. ".enabled=" .. (enabled and "1" or "0")
	if not command_ok(UCI .. " set " .. shell_quote(assignment)) then return false end
	return command_ok(UCI .. " commit " .. shell_quote(CONFIG))
end

local function preflight()
	if not command_ok("test -x " .. shell_quote(PROG)) then return false, "Executable " .. PROG .. " is missing" end
	if not command_ok("test -x " .. shell_quote(INIT)) then return false, "Init script " .. INIT .. " is missing" end
	local iface = uci_get("axiom_interface", "br-lan")
	if not interface_exists(iface) then return false, "Axiom/RayNet interface " .. iface .. " does not exist" end
	local manage_ip = boolean_value(uci_get("manage_ip", "1"), true)
	local ipaddr = uci_get("ipaddr", "198.18.0.1")
	local prefix = tonumber(uci_get("prefix", "21")) or 21
	if not manage_ip and not interface_has_cidr(iface, ipaddr .. "/" .. tostring(math.floor(prefix))) then
		return false, ipaddr .. "/" .. tostring(math.floor(prefix)) .. " is not present on " .. iface
	end
	if not manage_ip then os.remove(MANAGED_IP_FILE) end
	return true
end

local function schedule(sequence)
	local detached = "( " .. sequence .. " ) >" .. shell_quote(ACTION_LOG) .. " 2>&1 </dev/null &"
	local rc = os.execute(detached)
	return rc == 0
end

local function safe_response(self, label, callback)
	local success, ok, message = pcall(callback)
	if not success then return self:ResponseOK({ ok = false, message = label .. " failed: " .. tostring(ok) }) end
	return self:ResponseOK({ ok = ok, message = message })
end

function Service:StartAction()
	return safe_response(self, "Start", function()
		local ok, message = preflight()
		if not ok then return false, message end
		if not set_enabled(true) then return false, "Could not enable YachtSense configuration" end
		local qinit = shell_quote(INIT)
		if not schedule("timeout 5 " .. qinit .. " enable; timeout 12 " .. qinit .. " start") then
			return false, "Could not schedule service start"
		end
		return true, "Service start scheduled"
	end)
end
Service:action("start", Service.StartAction)

function Service:RestartAction()
	return safe_response(self, "Restart", function()
		local ok, message = preflight()
		if not ok then return false, message end
		if not set_enabled(true) then return false, "Could not enable YachtSense configuration" end
		local qinit = shell_quote(INIT)
		if not schedule("timeout 12 " .. qinit .. " stop; timeout 5 " .. qinit .. " enable; timeout 12 " .. qinit .. " start") then
			return false, "Could not schedule service restart"
		end
		return true, "Service restart scheduled"
	end)
end
Service:action("restart", Service.RestartAction)

function Service:StopAction()
	return safe_response(self, "Stop", function()
		if not set_enabled(false) then return false, "Could not disable YachtSense configuration" end
		if not command_ok("test -x " .. shell_quote(INIT)) then return true, "Service disabled; init script is not present" end
		local qinit = shell_quote(INIT)
		if not schedule("timeout 12 " .. qinit .. " stop; timeout 5 " .. qinit .. " disable") then
			return false, "Could not schedule service stop"
		end
		return true, "Service stop scheduled"
	end)
end
Service:action("stop", Service.StopAction)

return Service

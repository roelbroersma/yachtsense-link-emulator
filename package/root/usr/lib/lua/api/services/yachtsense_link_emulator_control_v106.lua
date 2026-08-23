-- Verified YachtSense Link Emulator service controls for RutOS 7.
--
-- Start/restart only report success when the same daemon PID remains alive.
-- When address management is disabled, package IP ownership is relinquished
-- before restart so an existing RayNet address is not removed by the init script.

local FunctionService = require("api/FunctionService")
local uci = require("uci")

local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local INIT = "/etc/init.d/yachtsense-link-emulator"
local PROCESS = "yachtsense-link-emulator"
local LOG_TAG = "yachtsense-link-emulator"
local MANAGED_IP_FILE = "/var/run/yachtsense-link-emulator.managed-ip"

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

local function interface_addresses(name)
	local result = {}
	if not interface_exists(name) then return result end
	local output = command_output("ip -o -4 addr show dev " .. shell_quote(name))
	for address in output:gmatch("inet%s+([%d%.]+/%d+)") do result[#result + 1] = address end
	return result
end

local function interface_has_cidr(name, wanted)
	for _, address in ipairs(interface_addresses(name)) do
		if address == wanted then return true end
	end
	return false
end

local function read_config()
	local cursor = uci.cursor()
	local remote_interfaces = cursor:get_list(CONFIG, SECTION, "remote_interface")
	if type(remote_interfaces) ~= "table" or #remote_interfaces == 0 then remote_interfaces = { "br-lan" } end
	local config = {
		axiom_interface = cursor:get(CONFIG, SECTION, "axiom_interface") or "br-lan",
		manage_ip = boolean_value(cursor:get(CONFIG, SECTION, "manage_ip"), true),
		ipaddr = cursor:get(CONFIG, SECTION, "ipaddr") or "198.18.0.1",
		prefix = tonumber(cursor:get(CONFIG, SECTION, "prefix") or "21") or 21,
		remote_interfaces = remote_interfaces
	}
	cursor:unload(CONFIG)
	return config
end

local function preflight()
	if not command_ok("test -x /usr/sbin/yachtsense-link-emulator") then
		return false, "Executable /usr/sbin/yachtsense-link-emulator is missing", nil
	end
	local config = read_config()
	if not interface_exists(config.axiom_interface) then
		return false, "Axiom/RayNet interface " .. config.axiom_interface .. " does not exist", config
	end
	local wanted = config.ipaddr .. "/" .. tostring(math.floor(config.prefix))
	if not config.manage_ip and not interface_has_cidr(config.axiom_interface, wanted) then
		return false, wanted .. " is not present on " .. config.axiom_interface .. "; enable Manage RayNet IPv4 address or select the correct Axiom interface", config
	end
	for _, name in ipairs(config.remote_interfaces) do
		name = trim(name)
		if not interface_exists(name) then return false, "Raymarine app interface " .. name .. " does not exist", config end
		if name ~= config.axiom_interface and #interface_addresses(name) == 0 then
			return false, "Raymarine app interface " .. name .. " has no IPv4 address", config
		end
	end
	return true, nil, config
end

local function set_enabled(enabled)
	local cursor = uci.cursor()
	if not cursor:get(CONFIG, SECTION) then cursor:section(CONFIG, "emulator", SECTION, {}) end
	cursor:set(CONFIG, SECTION, "enabled", enabled and "1" or "0")
	cursor:commit(CONFIG)
	cursor:unload(CONFIG)
end

local function latest_service_error()
	local line = command_output("logread -e " .. shell_quote(LOG_TAG) .. " | tail -n 7 | tr '\\n' ' '")
	return line ~= "" and line or "service exited immediately; no service log was produced"
end

local function stable_pid()
	os.execute("sleep 1")
	local first = command_output("pidof " .. shell_quote(PROCESS) .. " | awk '{print $1}'")
	if first == "" then return false end
	os.execute("sleep 2")
	local second = command_output("pidof " .. shell_quote(PROCESS) .. " | awk '{print $1}'")
	return second ~= "" and second == first
end

local function start_or_restart(label)
	local ok, message, config = preflight()
	if not ok then return false, message end
	set_enabled(true)
	if config and not config.manage_ip then os.remove(MANAGED_IP_FILE) end
	os.execute(INIT .. " enable >/dev/null 2>&1")
	os.execute(INIT .. " restart >/dev/null 2>&1")
	if not stable_pid() then return false, "Service did not stay running: " .. latest_service_error() end
	return true, label
end

function Service:StartAction()
	local ok, message = start_or_restart("Service started")
	return self:ResponseOK({ ok = ok, message = message })
end
Service:action("start", Service.StartAction)

function Service:RestartAction()
	local ok, message = start_or_restart("Service restarted")
	return self:ResponseOK({ ok = ok, message = message })
end
Service:action("restart", Service.RestartAction)

function Service:StopAction()
	os.execute(INIT .. " stop >/dev/null 2>&1")
	os.execute(INIT .. " disable >/dev/null 2>&1")
	set_enabled(false)
	return self:ResponseOK({ ok = true, message = "Service stopped" })
end
Service:action("stop", Service.StopAction)

return Service

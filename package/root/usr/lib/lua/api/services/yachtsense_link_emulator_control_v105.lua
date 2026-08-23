-- Verified service controls for the YachtSense Link Emulator.
--
-- RutOS/procd may accept a start request even when the child exits immediately.
-- These actions wait briefly and only report success when the daemon stays up.

local FunctionService = require("api/FunctionService")
local uci = require("uci")

local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local INIT = "/etc/init.d/yachtsense-link-emulator"
local PROCESS = "yachtsense-link-emulator"
local LOG_TAG = "yachtsense-link-emulator"

local function trim(value)
	if value == nil then
		return ""
	end
	return tostring(value):match("^%s*(.-)%s*$") or ""
end

local function shell_quote(value)
	return "'" .. tostring(value or ""):gsub("'", "'\\''") .. "'"
end

local function command_output(command)
	local pipe = io.popen(command .. " 2>/dev/null")
	if not pipe then
		return ""
	end
	local output = pipe:read("*a") or ""
	pipe:close()
	return output:gsub("%s+$", "")
end

local function command_ok(command)
	local pipe = io.popen("(" .. command .. ") >/dev/null 2>&1; printf '%s' $?")
	if not pipe then
		return false
	end
	local status = pipe:read("*a") or "1"
	pipe:close()
	return tonumber(status) == 0
end

local function boolean_value(value, fallback)
	if value == true or value == 1 or value == "1" then
		return true
	end
	if value == false or value == 0 or value == "0" then
		return false
	end
	if value == nil then
		return fallback
	end
	local normalized = trim(value):lower()
	if normalized == "true" or normalized == "yes" or normalized == "on" or normalized == "enabled" then
		return true
	end
	if normalized == "false" or normalized == "no" or normalized == "off" or normalized == "disabled" then
		return false
	end
	return fallback
end

local function read_uci_list(cursor, option, fallback)
	local value = cursor:get(CONFIG, SECTION, option)
	if type(value) == "table" then
		return value
	end
	if type(value) == "string" and trim(value) ~= "" then
		return { trim(value) }
	end
	return fallback
end

local function interface_exists(name)
	name = trim(name)
	if #name < 1 or #name > 64 or not name:match("^[%w%._:@%-]+$") then
		return false
	end
	local file = io.open("/sys/class/net/" .. name .. "/ifindex", "r")
	if not file then
		return false
	end
	file:close()
	return true
end

local function interface_addresses(name)
	local result = {}
	if not interface_exists(name) then
		return result
	end
	local output = command_output("ip -o -4 addr show dev " .. shell_quote(name))
	for address in output:gmatch("inet%s+([%d%.]+/%d+)") do
		result[#result + 1] = address
	end
	return result
end

local function interface_has_cidr(name, wanted)
	for _, address in ipairs(interface_addresses(name)) do
		if address == wanted then
			return true
		end
	end
	return false
end

local function read_config()
	local cursor = uci.cursor()
	local config = {
		axiom_interface = cursor:get(CONFIG, SECTION, "axiom_interface") or "br-lan",
		manage_ip = boolean_value(cursor:get(CONFIG, SECTION, "manage_ip"), true),
		ipaddr = cursor:get(CONFIG, SECTION, "ipaddr") or "198.18.0.1",
		prefix = tonumber(cursor:get(CONFIG, SECTION, "prefix") or "21") or 21,
		remote_interfaces = read_uci_list(cursor, "remote_interface", { "br-lan" })
	}
	cursor:unload(CONFIG)
	return config
end

local function preflight()
	if not command_ok("test -x /usr/sbin/yachtsense-link-emulator") then
		return false, "Executable /usr/sbin/yachtsense-link-emulator is missing"
	end
	local config = read_config()
	if not interface_exists(config.axiom_interface) then
		return false, "Axiom/RayNet interface " .. config.axiom_interface .. " does not exist"
	end
	local wanted = config.ipaddr .. "/" .. tostring(math.floor(config.prefix))
	if not config.manage_ip and not interface_has_cidr(config.axiom_interface, wanted) then
		return false, wanted .. " is not present on " .. config.axiom_interface .. "; enable Manage RayNet IPv4 address or select the correct Axiom interface"
	end
	for _, name in ipairs(config.remote_interfaces) do
		name = trim(name)
		if not interface_exists(name) then
			return false, "Raymarine app interface " .. name .. " does not exist"
		end
		if name ~= config.axiom_interface and #interface_addresses(name) == 0 then
			return false, "Raymarine app interface " .. name .. " has no IPv4 address"
		end
	end
	return true
end

local function set_enabled(enabled)
	local cursor = uci.cursor()
	if not cursor:get(CONFIG, SECTION) then
		cursor:section(CONFIG, "emulator", SECTION, {})
	end
	cursor:set(CONFIG, SECTION, "enabled", enabled and "1" or "0")
	cursor:commit(CONFIG)
	cursor:unload(CONFIG)
end

local function latest_service_error()
	local line = command_output("logread -e " .. shell_quote(LOG_TAG) .. " | tail -n 5 | tr '\\n' ' '")
	return line ~= "" and line or "service exited immediately; no service log was produced"
end

local function stays_running()
	os.execute("sleep 1")
	if not command_ok("pidof " .. shell_quote(PROCESS)) then
		return false
	end
	os.execute("sleep 1")
	return command_ok("pidof " .. shell_quote(PROCESS))
end

function Service:StartAction()
	local ok, message = preflight()
	if not ok then
		return self:ResponseOK({ ok = false, message = message })
	end
	set_enabled(true)
	os.execute(INIT .. " enable >/dev/null 2>&1")
	os.execute(INIT .. " restart >/dev/null 2>&1")
	if not stays_running() then
		return self:ResponseOK({ ok = false, message = "Service did not stay running: " .. latest_service_error() })
	end
	return self:ResponseOK({ ok = true, message = "Service started" })
end
Service:action("start", Service.StartAction)

function Service:RestartAction()
	local ok, message = preflight()
	if not ok then
		return self:ResponseOK({ ok = false, message = message })
	end
	set_enabled(true)
	os.execute(INIT .. " enable >/dev/null 2>&1")
	os.execute(INIT .. " restart >/dev/null 2>&1")
	if not stays_running() then
		return self:ResponseOK({ ok = false, message = "Service did not stay running: " .. latest_service_error() })
	end
	return self:ResponseOK({ ok = true, message = "Service restarted" })
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

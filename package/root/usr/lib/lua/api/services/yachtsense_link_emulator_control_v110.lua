-- YachtSense Link Emulator v1.0.10 service controls.
-- Reads and writes the same package-root UCI configuration as Save & Apply.

local FunctionService = require("api/FunctionService")
local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
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

local function uci_get_list(option, fallback)
	local output = uci_get(option, "")
	local result = {}
	for value in output:gmatch("%S+") do result[#result + 1] = value end
	if #result == 0 then for _, value in ipairs(fallback or {}) do result[#result + 1] = value end end
	return result
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
	for _, address in ipairs(interface_addresses(name)) do if address == wanted then return true end end
	return false
end

local function read_config()
	return {
		axiom_interface = uci_get("axiom_interface", "br-lan"),
		manage_ip = boolean_value(uci_get("manage_ip", "1"), true),
		ipaddr = uci_get("ipaddr", "198.18.0.1"),
		prefix = tonumber(uci_get("prefix", "21")) or 21,
		remote_interfaces = uci_get_list("remote_interface", { "br-lan" })
	}
end

local function preflight()
	if not command_ok("test -x " .. shell_quote(PROG)) then return false, "Executable " .. PROG .. " is missing", nil end
	if not command_ok("test -x " .. shell_quote(INIT)) then return false, "Init script " .. INIT .. " is missing", nil end
	local config = read_config()
	if not interface_exists(config.axiom_interface) then return false, "Axiom/RayNet interface " .. config.axiom_interface .. " does not exist", config end
	local wanted = config.ipaddr .. "/" .. tostring(math.floor(config.prefix))
	if not config.manage_ip and not interface_has_cidr(config.axiom_interface, wanted) then
		return false, wanted .. " is not present on " .. config.axiom_interface .. "; enable Manage RayNet IPv4 address or select the correct Axiom interface", config
	end
	for _, name in ipairs(config.remote_interfaces) do
		if not interface_exists(name) then return false, "Raymarine app interface " .. name .. " does not exist", config end
		if name ~= config.axiom_interface and #interface_addresses(name) == 0 then return false, "Raymarine app interface " .. name .. " has no IPv4 address", config end
	end
	return true, nil, config
end

local function set_enabled(enabled)
	local assignment = CONFIG .. "." .. SECTION .. ".enabled=" .. (enabled and "1" or "0")
	if not command_ok(UCI .. " set " .. shell_quote(assignment)) then return false end
	return command_ok(UCI .. " commit " .. shell_quote(CONFIG))
end

local function latest_service_error()
	local line = command_output("logread -e " .. shell_quote(LOG_TAG) .. " | tail -n 10 | tr '\\n' ' '")
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
	if not set_enabled(true) then return false, "Could not enable YachtSense configuration in " .. UCI_DIR end
	if config and not config.manage_ip then os.remove(MANAGED_IP_FILE) end
	if not command_ok(shell_quote(INIT) .. " enable") then return false, "Could not enable YachtSense autostart" end
	os.execute(shell_quote(INIT) .. " restart >/dev/null 2>&1")
	if not stable_pid() then return false, "Service did not stay running: " .. latest_service_error() end
	return true, label
end

local function safe_response(self, label, callback)
	local success, ok, message = pcall(callback)
	if not success then return self:ResponseOK({ ok = false, message = label .. " failed: " .. tostring(ok) }) end
	return self:ResponseOK({ ok = ok, message = message })
end

function Service:StartAction()
	return safe_response(self, "Start", function() return start_or_restart("Service started") end)
end
Service:action("start", Service.StartAction)

function Service:RestartAction()
	return safe_response(self, "Restart", function() return start_or_restart("Service restarted") end)
end
Service:action("restart", Service.RestartAction)

function Service:StopAction()
	return safe_response(self, "Stop", function()
		if command_ok("test -x " .. shell_quote(INIT)) then
			os.execute(shell_quote(INIT) .. " stop >/dev/null 2>&1")
			os.execute(shell_quote(INIT) .. " disable >/dev/null 2>&1")
		end
		if not set_enabled(false) then return false, "Service stopped, but configuration in " .. UCI_DIR .. " could not be disabled" end
		return true, "Service stopped"
	end)
end
Service:action("stop", Service.StopAction)

return Service

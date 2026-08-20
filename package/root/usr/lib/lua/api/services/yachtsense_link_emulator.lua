-- YachtSense Link Emulator RutOS API service.
--
-- The module connects the VuCI page to UCI configuration, procd service
-- controls, interface discovery, runtime checks and recent system log lines.

local FunctionService = require("api/FunctionService")
local uci = require("uci")

local Service = FunctionService:new()

-- Fixed names shared by the package, init script and VuCI descriptors.
local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local INIT = "/etc/init.d/yachtsense-link-emulator"
local PROCESS = "yachtsense-link-emulator"
local LOG_TAG = "yachtsense-link-emulator"
local PACKAGE = "tlt_custom_pkg_yachtsense-link-emulator"

-- Defaults mirror /etc/config/yachtsense_link_emulator. Repeating them here
-- keeps the status endpoint useful if one individual UCI option is missing.
local defaults = {
	enabled = "0",
	mdns_enabled = "1",
	web_enabled = "1",
	interface = "br-lan",
	manage_ip = "1",
	ipaddr = "198.18.0.1",
	prefix = "21",
	remove_ip_on_stop = "1",
	serial = "AF002A4",
	version = "V142.242.530",
	hostname = "yachtsense-main",
	instance = "yachtsense-main Settings",
	ttl = "120",
	health_port = "7777",
	log_lines = "24"
}

-- Normalize optional values before validation or storage.
local function trim(value)
	if value == nil then
		return ""
	end
	return tostring(value):match("^%s*(.-)%s*$") or ""
end

-- Quote one arbitrary value as a single POSIX shell argument.
local function shell_quote(value)
	return "'" .. tostring(value or ""):gsub("'", "'\\''") .. "'"
end

-- Capture stdout while suppressing diagnostics from the WebUI response.
local function command_output(command)
	local pipe = io.popen(command .. " 2>/dev/null")
	if not pipe then
		return ""
	end

	local output = pipe:read("*a") or ""
	pipe:close()
	return output:gsub("%s+$", "")
end

-- Execute a local command and return true only for exit status zero.
local function command_ok(command)
	local pipe = io.popen("(" .. command .. ") >/dev/null 2>&1; printf '%s' $?")
	if not pipe then
		return false
	end

	local status = pipe:read("*a") or "1"
	pipe:close()
	return tonumber(status) == 0
end

-- Accept booleans from JSON, UCI, numbers and common textual forms.
local function boolean_value(value, fallback)
	if value == nil then
		return fallback
	end
	if value == true or value == 1 or value == "1" then
		return true
	end
	if value == false or value == 0 or value == "0" then
		return false
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

-- Store booleans in the conventional UCI string format.
local function boolean_string(value)
	return boolean_value(value, false) and "1" or "0"
end

-- Validate a dotted-decimal IPv4 address without extra Lua modules.
local function valid_ipv4(value)
	value = trim(value)
	local a, b, c, d = value:match("^(%d+)%.(%d+)%.(%d+)%.(%d+)$")
	if not a then
		return false
	end

	for _, octet in ipairs({ a, b, c, d }) do
		local number = tonumber(octet)
		if not number or number < 0 or number > 255 then
			return false
		end
	end
	return true
end

-- Validate an integer inside an inclusive range.
local function valid_integer(value, minimum, maximum)
	local number = tonumber(value)
	return number
		and number == math.floor(number)
		and number >= minimum
		and number <= maximum
end

-- Restrict interface names before using them in sysfs paths or shell commands.
local function safe_interface_name(value)
	value = trim(value)
	return #value >= 1
		and #value <= 64
		and value:match("^[%w%._:@%-]+$") ~= nil
end

-- Require the selected interface to exist when a configuration is saved.
local function valid_interface(value)
	return safe_interface_name(value)
		and command_ok("test -d /sys/class/net/" .. shell_quote(trim(value)))
end

-- Validate the single DNS label used as the advertised .local hostname.
local function valid_hostname(value)
	value = trim(value)
	return #value >= 1
		and #value <= 63
		and value:match("^[%w][%w%-]*$") ~= nil
		and value:sub(-1) ~= "-"
end

-- Validate the suffix appended to the fixed E70640 product identifier.
local function valid_serial(value)
	value = trim(value)
	return #value >= 1
		and #value <= 32
		and value:match("^[%w_-]+$") ~= nil
end

-- Validate human-readable TXT and DNS-SD instance values.
local function valid_printable(value, maximum, allow_dot)
	value = trim(value)
	if #value < 1 or #value > maximum or value:find("[%c%z]") then
		return false
	end
	if not allow_dot and value:find("%.") then
		return false
	end
	return true
end

-- Read all UCI settings and convert typed fields for JSON output.
local function read_config()
	local cursor = uci.cursor()
	local result = {}

	for key, fallback in pairs(defaults) do
		result[key] = cursor:get(CONFIG, SECTION, key) or fallback
	end
	cursor:unload(CONFIG)

	result.enabled = boolean_value(result.enabled, false)
	result.mdns_enabled = boolean_value(result.mdns_enabled, true)
	result.web_enabled = boolean_value(result.web_enabled, true)
	result.manage_ip = boolean_value(result.manage_ip, true)
	result.remove_ip_on_stop = boolean_value(result.remove_ip_on_stop, true)
	result.prefix = tonumber(result.prefix) or 21
	result.ttl = tonumber(result.ttl) or 120
	result.health_port = tonumber(result.health_port) or 7777
	result.log_lines = tonumber(result.log_lines) or 24
	return result
end

-- Create the named UCI section if a user manually recreated the config file.
local function ensure_section(cursor)
	if not cursor:get(CONFIG, SECTION) then
		cursor:section(CONFIG, "emulator", SECTION, {})
	end
end

-- Return all IPv4 CIDRs currently assigned to one Linux interface.
local function interface_addresses(name)
	local addresses = {}
	if not safe_interface_name(name) then
		return addresses
	end

	local output = command_output("ip -o -4 addr show dev " .. shell_quote(name))
	for address in output:gmatch("inet%s+([%d%.]+/%d+)") do
		addresses[#addresses + 1] = address
	end
	return addresses
end

-- Build the interface selector directly from Linux sysfs.
local function list_interfaces()
	local result = {}
	local pipe = io.popen("ls -1 /sys/class/net 2>/dev/null | sort")
	if not pipe then
		return result
	end

	for name in pipe:lines() do
		if name ~= "lo" and safe_interface_name(name) then
			local mac_file = io.open("/sys/class/net/" .. name .. "/address", "r")
			local mac = ""
			if mac_file then
				mac = trim(mac_file:read("*l"))
				mac_file:close()
			end

			result[#result + 1] = {
				name = name,
				addresses = interface_addresses(name),
				mac = mac,
				up = command_ok("ip link show dev " .. shell_quote(name) .. " | grep -q 'state UP'")
			}
		end
	end
	pipe:close()
	return result
end

-- Report whether the exact configured address is present on the interface.
local function address_present(config)
	local wanted = config.ipaddr .. "/" .. tostring(config.prefix)
	for _, address in ipairs(interface_addresses(config.interface)) do
		if address == wanted then
			return true
		end
	end
	return false
end

-- Return a bounded number of recent matching system-log lines.
local function read_logs(limit)
	limit = tonumber(limit) or 24
	if limit < 5 then
		limit = 5
	elseif limit > 100 then
		limit = 100
	end

	local output = command_output("logread -e " .. shell_quote(LOG_TAG) .. " | tail -n " .. tostring(limit))
	local result = {}
	for line in (output .. "\n"):gmatch("(.-)\n") do
		if line ~= "" then
			result[#result + 1] = line
		end
	end
	return result
end

-- Read the installed IPK version without duplicating it in this Lua module.
local function package_version()
	local version = command_output(
		"opkg status " .. shell_quote(PACKAGE) .. " | awk -F': ' '/^Version:/{print $2; exit}'"
	)
	return version ~= "" and version or "development"
end

-- Read a concise RutOS/OpenWrt release identifier for diagnostics.
local function rutos_version()
	local release = command_output(
		"awk -F= '/^DISTRIB_RELEASE=/{gsub(/[\"\\047]/,\"\",$2); print $2; exit}' /etc/openwrt_release"
	)
	return release ~= "" and release or "unknown"
end

-- Combine configuration, process, network, HTTP and log information.
local function collect_status()
	local config = read_config()
	local running = command_ok("pidof " .. shell_quote(PROCESS))
	local interface_exists = safe_interface_name(config.interface)
		and command_ok("test -d /sys/class/net/" .. shell_quote(config.interface))
	local has_address = interface_exists and address_present(config)
	local web_url = "http://" .. config.ipaddr .. ":" .. tostring(config.health_port) .. "/"
	local web_reachable = false

	if running and config.web_enabled and has_address then
		web_reachable = command_ok("wget -q -T 1 -O /dev/null " .. shell_quote(web_url))
	end

	return {
		config = config,
		status = {
			running = running,
			autostart = command_ok(
				"find /etc/rc.d -maxdepth 1 -type l -name " ..
				shell_quote("S*yachtsense-link-emulator") .. " | grep -q ."
			),
			interface_exists = interface_exists,
			address_present = has_address,
			mdns_active = running and config.mdns_enabled and has_address,
			web_active = running and config.web_enabled and web_reachable,
			web_reachable = web_reachable,
			pid = command_output("pidof " .. shell_quote(PROCESS) .. " | awk '{print $1}'"),
			address = config.ipaddr .. "/" .. tostring(config.prefix),
			web_url = web_url,
			discovery = "_http._tcp.local / id=E70640 " .. config.serial,
			architecture = command_output("uname -m"),
			rutos = rutos_version(),
			package_version = package_version()
		},
		interfaces = list_interfaces(),
		logs = read_logs(config.log_lines)
	}
end

-- Persist a complete validated form and apply the master enabled state.
local function apply_config(data)
	local cursor = uci.cursor()
	ensure_section(cursor)

	local values = {
		enabled = boolean_string(data.enabled),
		mdns_enabled = boolean_string(data.mdns_enabled),
		web_enabled = boolean_string(data.web_enabled),
		interface = trim(data.interface),
		manage_ip = boolean_string(data.manage_ip),
		ipaddr = trim(data.ipaddr),
		prefix = tostring(math.floor(tonumber(data.prefix))),
		remove_ip_on_stop = boolean_string(data.remove_ip_on_stop),
		serial = trim(data.serial),
		version = trim(data.version),
		hostname = trim(data.hostname),
		instance = trim(data.instance),
		ttl = tostring(math.floor(tonumber(data.ttl))),
		health_port = tostring(math.floor(tonumber(data.health_port))),
		log_lines = tostring(math.floor(tonumber(data.log_lines)))
	}

	for key, value in pairs(values) do
		cursor:set(CONFIG, SECTION, key, value)
	end
	cursor:commit(CONFIG)
	cursor:unload(CONFIG)

	if values.enabled == "1" then
		os.execute(INIT .. " enable >/dev/null 2>&1")
		os.execute(INIT .. " restart >/dev/null 2>&1")
	else
		os.execute(INIT .. " stop >/dev/null 2>&1")
		os.execute(INIT .. " disable >/dev/null 2>&1")
	end
end

-- GET /api/yachtsense-link-emulator/status
function Service:GET_TYPE_status()
	return self:ResponseOK(collect_status())
end

-- POST /api/yachtsense-link-emulator/actions/save
function Service:SaveAction()
	local data = self.arguments.data or {}
	apply_config(data)
	return self:ResponseOK({ ok = true, message = "Configuration saved and applied" })
end

local save_action = Service:action("save", Service.SaveAction)

-- Register a required boolean field on the save action.
local function add_boolean_option(name)
	local option = save_action:option(name)
	option.require = true
	function option:validate(value)
		return boolean_value(value, nil) ~= nil
	end
end

add_boolean_option("enabled")
add_boolean_option("mdns_enabled")
add_boolean_option("web_enabled")
add_boolean_option("manage_ip")
add_boolean_option("remove_ip_on_stop")

local interface_option = save_action:option("interface")
interface_option.require = true
function interface_option:validate(value)
	return valid_interface(value)
end

local ip_option = save_action:option("ipaddr")
ip_option.require = true
function ip_option:validate(value)
	return valid_ipv4(value)
end

local prefix_option = save_action:option("prefix")
prefix_option.require = true
function prefix_option:validate(value)
	return valid_integer(value, 0, 32)
end

local port_option = save_action:option("health_port")
port_option.require = true
function port_option:validate(value)
	return valid_integer(value, 1, 65535)
end

local ttl_option = save_action:option("ttl")
ttl_option.require = true
function ttl_option:validate(value)
	return valid_integer(value, 1, 86400)
end

local logs_option = save_action:option("log_lines")
logs_option.require = true
function logs_option:validate(value)
	return valid_integer(value, 5, 100)
end

local serial_option = save_action:option("serial")
serial_option.require = true
serial_option.maxlength = 32
function serial_option:validate(value)
	return valid_serial(value)
end

local version_option = save_action:option("version")
version_option.require = true
version_option.maxlength = 64
function version_option:validate(value)
	return valid_printable(value, 64, true)
end

local hostname_option = save_action:option("hostname")
hostname_option.require = true
hostname_option.maxlength = 63
function hostname_option:validate(value)
	return valid_hostname(value)
end

local instance_option = save_action:option("instance")
instance_option.require = true
instance_option.maxlength = 63
function instance_option:validate(value)
	return valid_printable(value, 63, false)
end

-- Restart without changing the saved enabled state.
function Service:RestartAction()
	local config = read_config()
	if not config.enabled then
		return self:ResponseOK({ ok = false, message = "Service is disabled" })
	end

	local ok = command_ok(INIT .. " restart")
	return self:ResponseOK({ ok = ok, message = ok and "Service restarted" or "Restart failed" })
end
Service:action("restart", Service.RestartAction)

-- Start now and persist both the UCI master switch and boot-time enablement.
function Service:StartAction()
	local cursor = uci.cursor()
	ensure_section(cursor)
	cursor:set(CONFIG, SECTION, "enabled", "1")
	cursor:commit(CONFIG)
	cursor:unload(CONFIG)

	os.execute(INIT .. " enable >/dev/null 2>&1")
	local ok = command_ok(INIT .. " restart")
	return self:ResponseOK({ ok = ok, message = ok and "Service started" or "Start failed" })
end
Service:action("start", Service.StartAction)

-- Stop now and persist both the UCI master switch and boot-time disablement.
function Service:StopAction()
	local ok = command_ok(INIT .. " stop")
	os.execute(INIT .. " disable >/dev/null 2>&1")

	local cursor = uci.cursor()
	ensure_section(cursor)
	cursor:set(CONFIG, SECTION, "enabled", "0")
	cursor:commit(CONFIG)
	cursor:unload(CONFIG)

	return self:ResponseOK({ ok = ok, message = ok and "Service stopped" or "Stop completed with an error" })
end
Service:action("stop", Service.StopAction)

return Service

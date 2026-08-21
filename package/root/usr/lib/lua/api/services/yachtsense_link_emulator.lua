-- YachtSense Link Emulator RutOS API service.
--
-- The module connects the VuCI page to UCI configuration, procd controls,
-- interface discovery, mDNS/Avahi diagnostics, daemon runtime state and logs.

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
local RUNTIME_STATE = "/var/run/yachtsense-link-emulator.runtime"

-- Defaults mirror /etc/config/yachtsense_link_emulator. Repeating them here
-- keeps the status endpoint useful if one individual UCI option is missing.
local defaults = {
	enabled = "0",
	mdns_enabled = "1",
	web_enabled = "1",
	axiom_interface = "br-lan",
	relay_mode = "auto",
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
	log_level = "info",
	log_lines = "40"
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

local function valid_integer(value, minimum, maximum)
	local number = tonumber(value)
	return number
		and number == math.floor(number)
		and number >= minimum
		and number <= maximum
end

-- Restrict Linux interface names before using them in sysfs or shell commands.
local function safe_interface_name(value)
	value = trim(value)
	return #value >= 1
		and #value <= 64
		and value:match("^[%w%._:@%-]+$") ~= nil
end

local function valid_interface(value)
	return safe_interface_name(value)
		and command_ok("test -d /sys/class/net/" .. shell_quote(trim(value)))
end

local function valid_hostname(value)
	value = trim(value)
	return #value >= 1
		and #value <= 63
		and value:match("^[%w][%w%-]*$") ~= nil
		and value:sub(-1) ~= "-"
end

local function valid_serial(value)
	value = trim(value)
	return #value >= 1
		and #value <= 32
		and value:match("^[%w_-]+$") ~= nil
end

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

local function valid_relay_mode(value)
	value = trim(value):lower()
	return value == "auto" or value == "force" or value == "disabled"
end

local function valid_log_level(value)
	value = trim(value):lower()
	return value == "info" or value == "debug"
end

-- Normalize an interface array received from JSON and remove duplicates.
local function normalize_interface_list(value)
	if type(value) ~= "table" then
		return nil
	end
	local result = {}
	local seen = {}
	for _, item in ipairs(value) do
		item = trim(item)
		if not valid_interface(item) then
			return nil
		end
		if not seen[item] then
			seen[item] = true
			result[#result + 1] = item
		end
	end
	return result
end

-- Read a UCI list regardless of whether the binding returns a string or table.
local function read_uci_list(cursor, option, fallback)
	local value = cursor:get(CONFIG, SECTION, option)
	if type(value) == "table" then
		local result = {}
		for _, item in ipairs(value) do
			if trim(item) ~= "" then
				result[#result + 1] = trim(item)
			end
		end
		return result
	end
	if type(value) == "string" and trim(value) ~= "" then
		return { trim(value) }
	end
	return fallback
end

-- Read all UCI settings and convert typed fields for JSON output.
local function read_config()
	local cursor = uci.cursor()
	local result = {}
	for key, fallback in pairs(defaults) do
		result[key] = cursor:get(CONFIG, SECTION, key) or fallback
	end
	result.remote_interfaces = read_uci_list(cursor, "remote_interface", { "br-lan" })
	cursor:unload(CONFIG)

	result.enabled = boolean_value(result.enabled, false)
	result.mdns_enabled = boolean_value(result.mdns_enabled, true)
	result.web_enabled = boolean_value(result.web_enabled, true)
	result.manage_ip = boolean_value(result.manage_ip, true)
	result.remove_ip_on_stop = boolean_value(result.remove_ip_on_stop, true)
	result.prefix = tonumber(result.prefix) or 21
	result.ttl = tonumber(result.ttl) or 120
	result.health_port = tonumber(result.health_port) or 7777
	result.log_lines = tonumber(result.log_lines) or 40
	return result
end

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

-- Build the interface selectors directly from Linux sysfs.
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

local function address_present(config)
	local wanted = config.ipaddr .. "/" .. tostring(config.prefix)
	for _, address in ipairs(interface_addresses(config.axiom_interface)) do
		if address == wanted then
			return true
		end
	end
	return false
end

-- Return a bounded number of recent matching system-log lines.
local function read_logs(limit)
	limit = tonumber(limit) or 40
	if limit < 5 then
		limit = 5
	elseif limit > 200 then
		limit = 200
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

local function package_version()
	local version = command_output(
		"opkg status " .. shell_quote(PACKAGE) .. " | awk -F': ' '/^Version:/{print $2; exit}'"
	)
	return version ~= "" and version or "development"
end

local function rutos_version()
	local release = command_output(
		"awk -F= '/^DISTRIB_RELEASE=/{gsub(/[\"\\047]/,\"\",$2); print $2; exit}' /etc/openwrt_release"
	)
	return release ~= "" and release or "unknown"
end

-- Read the daemon's simple key=value runtime state file.
local function read_runtime_state()
	local result = {}
	local file = io.open(RUNTIME_STATE, "r")
	if not file then
		return result
	end
	for line in file:lines() do
		local key, value = line:match("^([%w_]+)=(.*)$")
		if key then
			result[key] = value
		end
	end
	file:close()
	return result
end

-- Return active UDP/5353 listeners without interpreting every listener as a
-- reflector. Avahi and umdns are reported separately below.
local function mdns_listener_lines()
	local output = command_output(
		"if command -v ss >/dev/null 2>&1; then " ..
		"ss -lunp 2>/dev/null | grep -E '(:5353|\\.5353)[[:space:]]' | head -n 12; " ..
		"elif command -v netstat >/dev/null 2>&1; then " ..
		"netstat -lnup 2>/dev/null | grep -E '(:5353|\\.5353)[[:space:]]' | head -n 12; fi"
	)
	local result = {}
	for line in (output .. "\n"):gmatch("(.-)\n") do
		if trim(line) ~= "" then
			result[#result + 1] = trim(line)
		end
	end
	return result
end

-- Inspect Avahi's actual reflector setting. A running Avahi daemon with
-- enable-reflector=no is just a responder and is not considered a conflict.
local function avahi_reflector_active()
	if not command_ok("pidof avahi-daemon") then
		return false
	end
	return command_ok(
		"for f in /etc/avahi/avahi-daemon.conf /etc/avahi/avahi-daemon.conf.d/*.conf; do " ..
		"[ -r \"$f\" ] || continue; " ..
		"grep -Eiq '^[[:space:]]*enable-reflector[[:space:]]*=[[:space:]]*(yes|true|1)[[:space:]]*([#;].*)?$' \"$f\" && exit 0; " ..
		"done; exit 1"
	)
end

local function mdns_environment()
	local listeners = mdns_listener_lines()
	local avahi_running = command_ok("pidof avahi-daemon")
	local umdns_running = command_ok("pidof umdns")
	local reflector = avahi_reflector_active()
	return {
		port_in_use = #listeners > 0,
		listeners = listeners,
		avahi_running = avahi_running,
		avahi_reflector = reflector,
		umdns_running = umdns_running,
		message = reflector
			and "Existing Avahi reflector detected; Automatic mode will not start a second reflector."
			or (#listeners > 0
				and "UDP/5353 listener detected. A local mDNS responder is not automatically a reflector."
				or "No existing UDP/5353 listener detected.")
	}
end

-- Combine configuration, process, network, mDNS environment and daemon state.
local function collect_status()
	local config = read_config()
	local running = command_ok("pidof " .. shell_quote(PROCESS))
	local axiom_interface_exists = valid_interface(config.axiom_interface)
	local has_address = axiom_interface_exists and address_present(config)
	local web_url = "http://" .. config.ipaddr .. ":" .. tostring(config.health_port) .. "/"
	local web_reachable = false
	if running and config.web_enabled and has_address then
		web_reachable = command_ok("wget -q -T 1 -O /dev/null " .. shell_quote(web_url))
	end

	local runtime = read_runtime_state()
	local mdns = mdns_environment()
	local remote_present = true
	for _, name in ipairs(config.remote_interfaces) do
		if not valid_interface(name) then
			remote_present = false
			break
		end
	end

	return {
		config = config,
		status = {
			running = running,
			autostart = command_ok(
				"find /etc/rc.d -maxdepth 1 -type l -name " ..
				shell_quote("S*yachtsense-link-emulator") .. " | grep -q ."
			),
			axiom_interface_exists = axiom_interface_exists,
			remote_interfaces_present = remote_present,
			address_present = has_address,
			mdns_active = running and config.mdns_enabled and has_address,
			web_active = running and config.web_enabled and web_reachable,
			web_reachable = web_reachable,
			relay_active = boolean_value(runtime.relay_active, false),
			relay_reason = runtime.relay_reason or "not running",
			pid = command_output("pidof " .. shell_quote(PROCESS) .. " | awk '{print $1}'"),
			address = config.ipaddr .. "/" .. tostring(config.prefix),
			web_url = web_url,
			discovery = "_http._tcp.local / yachtsense-main / id=E70640 " .. config.serial,
			last_axiom_name = runtime.last_axiom_name or "",
			last_axiom_ip = runtime.last_axiom_ip or "",
			last_axiom_service = runtime.last_axiom_service or "",
			last_axiom_port = runtime.last_axiom_port or "",
			last_remote_ip = runtime.last_remote_ip or "",
			last_remote_iface = runtime.last_remote_iface or "",
			last_remote_service = runtime.last_remote_service or "",
			last_activity = runtime.last_activity or "",
			architecture = command_output("uname -m"),
			rutos = rutos_version(),
			package_version = package_version()
		},
		mdns_environment = mdns,
		interfaces = list_interfaces(),
		logs = read_logs(config.log_lines)
	}
end

-- Persist a complete validated form and apply the master enabled state.
local function apply_config(data)
	local remote_interfaces = normalize_interface_list(data.remote_interfaces) or {}
	local cursor = uci.cursor()
	ensure_section(cursor)

	local values = {
		enabled = boolean_string(data.enabled),
		mdns_enabled = boolean_string(data.mdns_enabled),
		web_enabled = boolean_string(data.web_enabled),
		axiom_interface = trim(data.axiom_interface),
		relay_mode = trim(data.relay_mode):lower(),
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
		log_level = trim(data.log_level):lower(),
		log_lines = tostring(math.floor(tonumber(data.log_lines)))
	}

	for key, value in pairs(values) do
		cursor:set(CONFIG, SECTION, key, value)
	end
	-- libuci's Lua binding accepts a table here and stores a UCI list option.
	cursor:set(CONFIG, SECTION, "remote_interface", remote_interfaces)
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

local axiom_interface_option = save_action:option("axiom_interface")
axiom_interface_option.require = true
function axiom_interface_option:validate(value)
	return valid_interface(value)
end

local remote_interfaces_option = save_action:option("remote_interfaces")
remote_interfaces_option.require = true
function remote_interfaces_option:validate(value)
	if type(value) ~= "table" or #value < 1 or #value > 16 then
		return false
	end
	return normalize_interface_list(value) ~= nil
end

local relay_mode_option = save_action:option("relay_mode")
relay_mode_option.require = true
function relay_mode_option:validate(value)
	return valid_relay_mode(value)
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
	return valid_integer(value, 5, 200)
end

local log_level_option = save_action:option("log_level")
log_level_option.require = true
function log_level_option:validate(value)
	return valid_log_level(value)
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

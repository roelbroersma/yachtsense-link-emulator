-- Stable read-only status API for YachtSense Link Emulator v1.0.10.
-- Uses the package-root UCI directory and one kernel IPv4 snapshot per refresh.

local FunctionService = require("api/FunctionService")
local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local PROCESS = "yachtsense-link-emulator"
local PACKAGE = "tlt_custom_pkg_yachtsense-link-emulator"
local LOG_TAG = "yachtsense-link-emulator"
local RUNTIME_STATE = "/var/run/yachtsense-link-emulator.runtime"

local defaults = {
	enabled = "0", mdns_enabled = "1", web_enabled = "1",
	axiom_interface = "br-lan", relay_mode = "auto", manage_ip = "1",
	ipaddr = "198.18.0.1", prefix = "21", remove_ip_on_stop = "1",
	serial = "AF002A4", version = "V142.242.530", hostname = "yachtsense-main",
	instance = "yachtsense-main Settings", ttl = "120", health_port = "7777",
	log_level = "info", log_lines = "40"
}

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

local function boolean_value(value, fallback)
	if value == nil then return fallback end
	if value == true or value == 1 or value == "1" then return true end
	if value == false or value == 0 or value == "0" then return false end
	local normalized = trim(value):lower()
	if normalized == "true" or normalized == "yes" or normalized == "on" or normalized == "enabled" then return true end
	if normalized == "false" or normalized == "no" or normalized == "off" or normalized == "disabled" then return false end
	return fallback
end

local function safe_interface_name(value)
	value = trim(value)
	return #value >= 1 and #value <= 64 and value:match("^[%w%._:@%-]+$") ~= nil
end

local function interface_exists(name)
	if not safe_interface_name(name) then return false end
	local file = io.open("/sys/class/net/" .. name .. "/ifindex", "r")
	if not file then return false end
	file:close()
	return true
end

local function read_file(path)
	local file = io.open(path, "r")
	if not file then return "" end
	local value = trim(file:read("*l") or "")
	file:close()
	return value
end

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

local function read_config()
	local result = {}
	for key, fallback in pairs(defaults) do result[key] = uci_get(key, fallback) end
	result.remote_interfaces = uci_get_list("remote_interface", { "br-lan" })
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

local function interface_snapshot()
	local output = command_output("ip -o -4 addr show")
	local addresses = {}
	for line in (output .. "\n"):gmatch("(.-)\n") do
		local name, cidr = line:match("^%d+:%s+([^%s]+)%s+inet%s+([%d%.]+/%d+)")
		if name and cidr then
			name = name:gsub("@.*$", "")
			addresses[name] = addresses[name] or {}
			addresses[name][#addresses[name] + 1] = cidr
		end
	end

	local result = {}
	local names = io.popen("ls -1 /sys/class/net 2>/dev/null | sort")
	if not names then return result, false, addresses end
	for name in names:lines() do
		if name ~= "lo" and safe_interface_name(name) then
			local state = read_file("/sys/class/net/" .. name .. "/operstate")
			result[#result + 1] = {
				name = name,
				addresses = addresses[name] or {},
				mac = read_file("/sys/class/net/" .. name .. "/address"),
				up = state == "up" or state == "unknown"
			}
		end
	end
	names:close()
	return result, true, addresses
end

local function has_cidr(addresses, name, wanted)
	for _, cidr in ipairs(addresses[name] or {}) do if cidr == wanted then return true end end
	return false
end

local function read_runtime_state()
	local result = {}
	local file = io.open(RUNTIME_STATE, "r")
	if not file then return result end
	for line in file:lines() do
		local key, value = line:match("^([%w_]+)=(.*)$")
		if key then result[key] = value end
	end
	file:close()
	return result
end

local function read_logs(limit)
	limit = tonumber(limit) or 40
	if limit < 5 then limit = 5 elseif limit > 200 then limit = 200 end
	local output = command_output("logread -e " .. shell_quote(LOG_TAG) .. " | tail -n " .. tostring(limit))
	local result = {}
	for line in (output .. "\n"):gmatch("(.-)\n") do if line ~= "" then result[#result + 1] = line end end
	return result
end

local function mdns_listener_lines()
	local output = command_output("if command -v ss >/dev/null 2>&1; then ss -lunp 2>/dev/null | grep -E '(:5353|\\.5353)[[:space:]]' | head -n 12; elif command -v netstat >/dev/null 2>&1; then netstat -lnup 2>/dev/null | grep -E '(:5353|\\.5353)[[:space:]]' | head -n 12; fi")
	local result = {}
	for line in (output .. "\n"):gmatch("(.-)\n") do if trim(line) ~= "" then result[#result + 1] = trim(line) end end
	return result
end

local function avahi_reflector_active()
	if not command_ok("pidof avahi-daemon") then return false end
	return command_ok("for f in /etc/avahi/avahi-daemon.conf /etc/avahi/avahi-daemon.conf.d/*.conf; do [ -r \"$f\" ] || continue; grep -Eiq '^[[:space:]]*enable-reflector[[:space:]]*=[[:space:]]*(yes|true|1)[[:space:]]*([#;].*)?$' \"$f\" && exit 0; done; exit 1")
end

local function mdns_environment()
	local listeners = mdns_listener_lines()
	local avahi_running = command_ok("pidof avahi-daemon")
	local umdns_running = command_ok("pidof umdns")
	local reflector = avahi_reflector_active()
	return {
		port_in_use = #listeners > 0, listeners = listeners,
		avahi_running = avahi_running, avahi_reflector = reflector, umdns_running = umdns_running,
		message = reflector and "Existing Avahi reflector detected; Automatic mode will not start a second reflector."
			or (#listeners > 0 and "UDP/5353 listener detected. A local mDNS responder is not automatically a reflector."
			or "No existing UDP/5353 listener detected.")
	}
end

local function package_version()
	local version = command_output("opkg status " .. shell_quote(PACKAGE) .. " | awk -F': ' '/^Version:/{print $2; exit}'")
	return version ~= "" and version or "development"
end

local function rutos_version()
	local release = command_output("awk -F= '/^DISTRIB_RELEASE=/{gsub(/[\"\\047]/,\"\",$2); print $2; exit}' /etc/openwrt_release")
	return release ~= "" and release or "unknown"
end

local function collect_status()
	local config = read_config()
	local interfaces, snapshot_ok, addresses = interface_snapshot()
	local running = command_ok("pidof " .. shell_quote(PROCESS))
	local axiom_exists = interface_exists(config.axiom_interface)
	local wanted = config.ipaddr .. "/" .. tostring(config.prefix)
	local has_address = axiom_exists and has_cidr(addresses, config.axiom_interface, wanted)
	local web_url = "http://" .. config.ipaddr .. ":" .. tostring(config.health_port) .. "/"
	local web_reachable = running and config.web_enabled and has_address and command_ok("wget -q -T 1 -O /dev/null " .. shell_quote(web_url))
	local runtime = read_runtime_state()
	local remote_present = true
	for _, name in ipairs(config.remote_interfaces) do if not interface_exists(name) then remote_present = false; break end end

	return {
		config = config, interfaces = interfaces, interfaces_snapshot_ok = snapshot_ok,
		logs = read_logs(config.log_lines), mdns_environment = mdns_environment(),
		status = {
			running = running,
			autostart = command_ok("ls /etc/rc.d/S*yachtsense-link-emulator >/dev/null 2>&1"),
			axiom_interface_exists = axiom_exists, remote_interfaces_present = remote_present,
			address_present = has_address, mdns_active = running and config.mdns_enabled and has_address,
			web_active = running and config.web_enabled and web_reachable, web_reachable = web_reachable,
			relay_active = boolean_value(runtime.relay_active, false), relay_reason = runtime.relay_reason or "not running",
			pid = command_output("pidof " .. shell_quote(PROCESS) .. " | awk '{print $1}'"),
			address = wanted, web_url = web_url,
			discovery = "_http._tcp.local / yachtsense-main / id=E70640 " .. config.serial,
			last_axiom_name = runtime.last_axiom_name or "", last_axiom_ip = runtime.last_axiom_ip or "",
			last_axiom_service = runtime.last_axiom_service or "", last_axiom_port = runtime.last_axiom_port or "",
			last_remote_ip = runtime.last_remote_ip or "", last_remote_iface = runtime.last_remote_iface or "",
			last_remote_service = runtime.last_remote_service or "", last_activity = runtime.last_activity or "",
			architecture = command_output("uname -m"), rutos = rutos_version(), package_version = package_version(),
			uci_config_dir = UCI_DIR
		}
	}
end

function Service:GET_TYPE_status()
	local success, result = pcall(collect_status)
	if not success then return self:ResponseOK({ ok = false, error = "Status collection failed: " .. tostring(result) }) end
	return self:ResponseOK(result)
end

return Service

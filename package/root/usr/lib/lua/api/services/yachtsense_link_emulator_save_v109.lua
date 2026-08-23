-- YachtSense Link Emulator v1.0.9 configuration writer.
--
-- This endpoint intentionally avoids RutOS' firmware-dependent Lua UCI methods.
-- Every scalar and list value is written through the native `uci` CLI.

local FunctionService = require("api/FunctionService")
local json = require("luci.jsonc")

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

local function package_root()
	local root = command_output("awk '$1 == \"dest\" && $2 == \"root\" { print $3; exit }' /etc/opkg.conf")
	if root == "" or root == "/" then return "" end
	return root:gsub("/+$", "")
end

local PREFIX = package_root()
local INIT = PREFIX .. "/etc/init.d/yachtsense-link-emulator"

local function boolean_value(value)
	if value == true or value == 1 or value == "1" then return true end
	if value == false or value == 0 or value == "0" then return false end
	local normalized = trim(value):lower()
	if normalized == "true" or normalized == "yes" or normalized == "on" or normalized == "enabled" then return true end
	if normalized == "false" or normalized == "no" or normalized == "off" or normalized == "disabled" then return false end
	return nil
end

local function boolean_string(value)
	return boolean_value(value) and "1" or "0"
end

local function valid_integer(value, minimum, maximum)
	local number = tonumber(value)
	return number ~= nil and number == math.floor(number) and number >= minimum and number <= maximum
end

local function valid_ipv4(value)
	local a, b, c, d = trim(value):match("^(%d+)%.(%d+)%.(%d+)%.(%d+)$")
	if not a then return false end
	for _, octet in ipairs({ a, b, c, d }) do
		local number = tonumber(octet)
		if not number or number < 0 or number > 255 then return false end
	end
	return true
end

local function valid_interface(value)
	value = trim(value)
	if #value < 1 or #value > 64 or not value:match("^[%w%._:@%-]+$") then return false end
	local file = io.open("/sys/class/net/" .. value .. "/ifindex", "r")
	if not file then return false end
	file:close()
	return true
end

local function interface_addresses(name)
	local result = {}
	if not valid_interface(name) then return result end
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

local function valid_hostname(value)
	value = trim(value)
	return #value >= 1 and #value <= 63 and value:match("^[%w][%w%-]*$") ~= nil and value:sub(-1) ~= "-"
end

local function valid_serial(value)
	value = trim(value)
	return #value >= 1 and #value <= 32 and value:match("^[%w_-]+$") ~= nil
end

local function valid_printable(value, maximum)
	value = trim(value)
	return #value >= 1 and #value <= maximum and not value:find("[%c%z]")
end

local function validate(data)
	if type(data) ~= "table" then return false, "Invalid configuration payload" end
	for _, key in ipairs({ "enabled", "mdns_enabled", "web_enabled", "manage_ip", "remove_ip_on_stop" }) do
		if boolean_value(data[key]) == nil then return false, "Invalid boolean value: " .. key end
	end

	local axiom_interface = trim(data.axiom_interface)
	if not valid_interface(axiom_interface) then return false, "Invalid Axiom/RayNet interface" end
	if not valid_ipv4(data.ipaddr) then return false, "Invalid RayNet IPv4 address" end
	if not valid_integer(data.prefix, 0, 32) then return false, "Invalid prefix length" end

	local wanted = trim(data.ipaddr) .. "/" .. tostring(math.floor(tonumber(data.prefix)))
	if boolean_value(data.manage_ip) == false and not interface_has_cidr(axiom_interface, wanted) then
		return false, wanted .. " is not present on " .. axiom_interface .. "; enable Manage RayNet IPv4 address or select the interface that already owns this address"
	end

	if type(data.remote_interfaces) ~= "table" or #data.remote_interfaces < 1 or #data.remote_interfaces > 16 then
		return false, "Select at least one Raymarine app interface"
	end
	for _, name in ipairs(data.remote_interfaces) do
		name = trim(name)
		if not valid_interface(name) then return false, "Invalid Raymarine app interface: " .. name end
		if name ~= axiom_interface and #interface_addresses(name) == 0 then
			return false, "Raymarine app interface " .. name .. " has no IPv4 address"
		end
	end

	local relay_mode = trim(data.relay_mode):lower()
	if relay_mode ~= "auto" and relay_mode ~= "force" and relay_mode ~= "disabled" then return false, "Invalid relay mode" end
	if not valid_integer(data.health_port, 1, 65535) then return false, "Invalid HTTP health port" end
	if not valid_integer(data.ttl, 1, 86400) then return false, "Invalid mDNS TTL" end
	if not valid_integer(data.log_lines, 5, 200) then return false, "Invalid log line count" end
	if trim(data.log_level):lower() ~= "info" and trim(data.log_level):lower() ~= "debug" then return false, "Invalid log level" end
	if not valid_serial(data.serial) then return false, "Invalid serial suffix" end
	if not valid_printable(data.version, 64) then return false, "Invalid firmware version" end
	if not valid_hostname(data.hostname) then return false, "Invalid mDNS hostname" end
	if not valid_printable(data.instance, 63) then return false, "Invalid service instance" end
	return true
end

local function uci_set(option, value)
	local assignment = CONFIG .. "." .. SECTION .. "." .. option .. "=" .. tostring(value)
	return command_ok("uci set " .. shell_quote(assignment))
end

local function write_config(data)
	if not command_ok("command -v uci") then return false, "RutOS uci command is unavailable" end
	if not command_ok("uci -q get " .. shell_quote(CONFIG .. "." .. SECTION)) then
		if not command_ok("uci set " .. shell_quote(CONFIG .. "." .. SECTION .. "=emulator")) then
			return false, "Could not create YachtSense configuration section"
		end
	end

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
		if not uci_set(key, value) then return false, "Could not store configuration option " .. key end
	end

	local list_key = CONFIG .. "." .. SECTION .. ".remote_interface"
	command_ok("uci -q delete " .. shell_quote(list_key))
	local seen = {}
	for _, interface in ipairs(data.remote_interfaces) do
		interface = trim(interface)
		if not seen[interface] then
			seen[interface] = true
			if not command_ok("uci add_list " .. shell_quote(list_key .. "=" .. interface)) then
				return false, "Could not store Raymarine app interface " .. interface
			end
		end
	end

	if not command_ok("uci commit " .. shell_quote(CONFIG)) then return false, "Could not commit YachtSense configuration" end
	if values.manage_ip == "0" then os.remove(MANAGED_IP_FILE) end

	if values.enabled == "1" then
		if not command_ok("test -x " .. shell_quote(INIT)) then return false, "Init script " .. INIT .. " is missing" end
		os.execute(shell_quote(INIT) .. " enable >/dev/null 2>&1")
		os.execute(shell_quote(INIT) .. " restart >/dev/null 2>&1")
	else
		if command_ok("test -x " .. shell_quote(INIT)) then
			os.execute(shell_quote(INIT) .. " stop >/dev/null 2>&1")
			os.execute(shell_quote(INIT) .. " disable >/dev/null 2>&1")
		end
	end
	return true
end

function Service:SaveAction()
	local success, ok, message = pcall(function()
		local raw = self.arguments.data and self.arguments.data.payload or ""
		local data = json.parse(raw)
		local valid, validation_message = validate(data)
		if not valid then return false, validation_message end
		return write_config(data)
	end)
	if not success then return self:ResponseOK({ ok = false, message = "Saving configuration failed: " .. tostring(ok) }) end
	return self:ResponseOK({ ok = ok, message = message or (ok and "Configuration saved and applied" or "Configuration could not be saved") })
end

local save_action = Service:action("save", Service.SaveAction)
local payload = save_action:option("payload")
payload.require = true
payload.maxlength = 16384
function payload:validate(value)
	return type(value) == "string" and #value > 1 and #value <= 16384
end

return Service

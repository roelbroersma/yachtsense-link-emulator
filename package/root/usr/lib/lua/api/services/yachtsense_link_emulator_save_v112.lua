-- YachtSense Link Emulator v1.0.12 configuration writer.
-- Configuration is committed synchronously, but service apply is deliberately
-- detached so a slow rc.common/procd round-trip can never hold the VuCI request.

local FunctionService = require("api/FunctionService")
local json = require("luci.jsonc")
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

local function interface_has_cidr(name, wanted)
	if not valid_interface(name) then return false end
	local output = command_output("ip -o -4 addr show dev " .. shell_quote(name))
	for address in output:gmatch("inet%s+([%d%.]+/%d+)") do
		if address == wanted then return true end
	end
	return false
end

local function validate(data)
	if type(data) ~= "table" then return false, "Invalid configuration payload" end
	for _, key in ipairs({ "enabled", "mdns_enabled", "web_enabled", "manage_ip", "remove_ip_on_stop" }) do
		if boolean_value(data[key]) == nil then return false, "Invalid boolean value: " .. key end
	end
	if not valid_interface(data.axiom_interface) then return false, "Invalid Axiom/RayNet interface" end
	if not valid_ipv4(data.ipaddr) then return false, "Invalid RayNet IPv4 address" end
	if not valid_integer(data.prefix, 0, 32) then return false, "Invalid prefix length" end
	if type(data.remote_interfaces) ~= "table" or #data.remote_interfaces < 1 or #data.remote_interfaces > 16 then
		return false, "Select at least one Raymarine app interface"
	end
	for _, name in ipairs(data.remote_interfaces) do
		if not valid_interface(name) then return false, "Invalid Raymarine app interface: " .. trim(name) end
	end
	if not valid_integer(data.health_port, 1, 65535) then return false, "Invalid HTTP health port" end
	if not valid_integer(data.ttl, 1, 86400) then return false, "Invalid mDNS TTL" end
	if not valid_integer(data.log_lines, 5, 200) then return false, "Invalid log line count" end
	local relay_mode = trim(data.relay_mode):lower()
	if relay_mode ~= "auto" and relay_mode ~= "force" and relay_mode ~= "disabled" then return false, "Invalid relay mode" end
	local log_level = trim(data.log_level):lower()
	if log_level ~= "info" and log_level ~= "debug" then return false, "Invalid log level" end
	if trim(data.serial) == "" or trim(data.hostname) == "" or trim(data.instance) == "" or trim(data.version) == "" then
		return false, "Identity fields may not be empty"
	end

	local wanted = trim(data.ipaddr) .. "/" .. tostring(math.floor(tonumber(data.prefix)))
	if boolean_value(data.manage_ip) == false and not interface_has_cidr(trim(data.axiom_interface), wanted) then
		return false, wanted .. " is not present on " .. trim(data.axiom_interface) .. "; enable Manage RayNet IPv4 address or select the correct Axiom interface"
	end
	return true
end

local function uci_set(option, value)
	local assignment = CONFIG .. "." .. SECTION .. "." .. option .. "=" .. tostring(value)
	return command_ok(UCI .. " set " .. shell_quote(assignment))
end

local function schedule_apply(enabled)
	if not command_ok("test -x " .. shell_quote(INIT)) then
		return false, "Init script " .. INIT .. " is missing"
	end
	local qinit = shell_quote(INIT)
	local sequence
	if enabled then
		sequence = "timeout 12 " .. qinit .. " stop; timeout 5 " .. qinit .. " enable; timeout 12 " .. qinit .. " start"
	else
		sequence = "timeout 12 " .. qinit .. " stop; timeout 5 " .. qinit .. " disable"
	end
	local detached = "( " .. sequence .. " ) >" .. shell_quote(ACTION_LOG) .. " 2>&1 </dev/null &"
	local rc = os.execute(detached)
	return rc == 0, rc == 0 and nil or "Could not schedule service apply"
end

local function write_config(data)
	if not command_ok("command -v uci") then return false, "RutOS uci command is unavailable" end
	if not command_ok("test -d " .. shell_quote(UCI_DIR)) then return false, "UCI configuration directory " .. UCI_DIR .. " is missing" end
	if not command_ok(UCI .. " -q get " .. shell_quote(CONFIG .. "." .. SECTION)) then
		if not command_ok(UCI .. " set " .. shell_quote(CONFIG .. "." .. SECTION .. "=emulator")) then
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
	command_ok(UCI .. " -q delete " .. shell_quote(list_key))
	local seen = {}
	for _, interface in ipairs(data.remote_interfaces) do
		interface = trim(interface)
		if not seen[interface] then
			seen[interface] = true
			if not command_ok(UCI .. " add_list " .. shell_quote(list_key .. "=" .. interface)) then
				return false, "Could not store Raymarine app interface " .. interface
			end
		end
	end
	if not command_ok(UCI .. " commit " .. shell_quote(CONFIG)) then return false, "Could not commit YachtSense configuration" end

	if values.manage_ip == "0" then os.remove(MANAGED_IP_FILE) end
	local scheduled, schedule_error = schedule_apply(values.enabled == "1")
	if not scheduled then return false, schedule_error end
	return true, values.enabled == "1" and "Configuration saved; service restart scheduled" or "Configuration saved; service stop scheduled"
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
	return self:ResponseOK({ ok = ok, message = message })
end

local save_action = Service:action("save", Service.SaveAction)
local payload = save_action:option("payload")
payload.require = true
payload.maxlength = 16384
function payload:validate(value)
	return type(value) == "string" and #value > 1 and #value <= 16384
end

return Service

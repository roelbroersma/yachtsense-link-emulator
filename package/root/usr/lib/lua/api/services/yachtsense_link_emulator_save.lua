-- Schema-safe YachtSense Link Emulator configuration writer for RutOS.
--
-- FunctionService must be told explicitly when an action option is a JSON list.
-- All form fields are declared here; detailed validation stays in the action so
-- invalid values return a useful message instead of an opaque HTTP 422.

local FunctionService = require("api/FunctionService")
local uci = require("uci")

local Service = FunctionService:new()

local CONFIG = "yachtsense_link_emulator"
local SECTION = "main"
local INIT = "/etc/init.d/yachtsense-link-emulator"

local function trim(value)
	if value == nil then
		return ""
	end
	return tostring(value):match("^%s*(.-)%s*$") or ""
end

local function boolean_value(value)
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
	return nil
end

local function boolean_string(value)
	return boolean_value(value) and "1" or "0"
end

local function valid_integer(value, minimum, maximum)
	local number = tonumber(value)
	return number ~= nil
		and number == math.floor(number)
		and number >= minimum
		and number <= maximum
end

local function valid_ipv4(value)
	local a, b, c, d = trim(value):match("^(%d+)%.(%d+)%.(%d+)%.(%d+)$")
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

local function valid_interface(value)
	value = trim(value)
	if #value < 1 or #value > 64 or not value:match("^[%w%._:@%-]+$") then
		return false
	end
	local file = io.open("/sys/class/net/" .. value .. "/ifindex", "r")
	if not file then
		return false
	end
	file:close()
	return true
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
	return #value >= 1 and #value <= 32 and value:match("^[%w_-]+$") ~= nil
end

local function valid_printable(value, maximum)
	value = trim(value)
	return #value >= 1 and #value <= maximum and not value:find("[%c%z]")
end

local function validate(data)
	for _, key in ipairs({ "enabled", "mdns_enabled", "web_enabled", "manage_ip", "remove_ip_on_stop" }) do
		if boolean_value(data[key]) == nil then
			return false, "Invalid boolean value: " .. key
		end
	end

	if not valid_interface(data.axiom_interface) then
		return false, "Invalid Axiom/RayNet interface"
	end

	if type(data.remote_interfaces) ~= "table" or #data.remote_interfaces < 1 or #data.remote_interfaces > 16 then
		return false, "Select at least one Raymarine app interface"
	end
	for _, name in ipairs(data.remote_interfaces) do
		if not valid_interface(name) then
			return false, "Invalid Raymarine app interface: " .. trim(name)
		end
	end

	local relay_mode = trim(data.relay_mode):lower()
	if relay_mode ~= "auto" and relay_mode ~= "force" and relay_mode ~= "disabled" then
		return false, "Invalid relay mode"
	end
	if not valid_ipv4(data.ipaddr) then
		return false, "Invalid RayNet IPv4 address"
	end
	if not valid_integer(data.prefix, 0, 32) then
		return false, "Invalid prefix length"
	end
	if not valid_integer(data.health_port, 1, 65535) then
		return false, "Invalid HTTP health port"
	end
	if not valid_integer(data.ttl, 1, 86400) then
		return false, "Invalid mDNS TTL"
	end
	if not valid_integer(data.log_lines, 5, 200) then
		return false, "Invalid log line count"
	end

	local log_level = trim(data.log_level):lower()
	if log_level ~= "info" and log_level ~= "debug" then
		return false, "Invalid log level"
	end
	if not valid_serial(data.serial) then
		return false, "Invalid serial suffix"
	end
	if not valid_printable(data.version, 64) then
		return false, "Invalid firmware version"
	end
	if not valid_hostname(data.hostname) then
		return false, "Invalid mDNS hostname"
	end
	if not valid_printable(data.instance, 63) then
		return false, "Invalid service instance"
	end

	return true
end

local function ensure_section(cursor)
	if not cursor:get(CONFIG, SECTION) then
		cursor:section(CONFIG, "emulator", SECTION, {})
	end
end

local function write_config(data)
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

	cursor:delete(CONFIG, SECTION, "remote_interface")
	local seen = {}
	for _, interface in ipairs(data.remote_interfaces) do
		interface = trim(interface)
		if not seen[interface] then
			seen[interface] = true
			cursor:add_list(CONFIG, SECTION, "remote_interface", interface)
		end
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

function Service:SaveAction()
	local data = self.arguments.data or {}
	local ok, message = validate(data)
	if not ok then
		return self:ResponseOK({ ok = false, message = message })
	end

	local success, err = pcall(write_config, data)
	if not success then
		return self:ResponseOK({ ok = false, message = "Saving configuration failed: " .. tostring(err) })
	end
	return self:ResponseOK({ ok = true, message = "Configuration saved and applied" })
end

local save_action = Service:action("save", Service.SaveAction)

-- Declare every field so FunctionService accepts the complete JSON form.
for _, name in ipairs({
	"enabled",
	"mdns_enabled",
	"web_enabled",
	"axiom_interface",
	"relay_mode",
	"manage_ip",
	"ipaddr",
	"prefix",
	"remove_ip_on_stop",
	"serial",
	"version",
	"hostname",
	"instance",
	"ttl",
	"health_port",
	"log_level",
	"log_lines"
}) do
	local option = save_action:option(name)
	option.require = true
end

-- RutOS FunctionService requires list=true for JSON arrays.
local remote_interfaces = save_action:option("remote_interfaces", { list = true })
remote_interfaces.require = true

return Service

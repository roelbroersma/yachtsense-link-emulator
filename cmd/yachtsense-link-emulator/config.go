package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const configName = "yachtsense_link_emulator"
const serviceName = "yachtsense-link-emulator"

var packageVersion = "development"

type Settings struct {
	Enabled          bool     `json:"enabled"`
	MDNS             bool     `json:"mdns_enabled"`
	HTTP             bool     `json:"web_enabled"`
	RaynetMode       string   `json:"raynet_mode"`
	AppMode          string   `json:"app_mode"`
	AxiomInterface   string   `json:"axiom_interface"`
	RemoteInterfaces []string `json:"remote_interfaces"`
	DiscoveryMode    string   `json:"discovery_mode"`
	ManageIP         bool     `json:"manage_ip"`
	IP               string   `json:"ipaddr"`
	Prefix           int      `json:"prefix"`
	RemoveIP         bool     `json:"remove_ip_on_stop"`
	Serial           string   `json:"serial"`
	Version          string   `json:"version"`
	Hostname         string   `json:"hostname"`
	Instance         string   `json:"instance"`
	TTL              int      `json:"ttl"`
	HealthPort       int      `json:"health_port"`
	LogLevel         string   `json:"log_level"`
	LogLines         int      `json:"log_lines"`
}
type Paths struct{ Prefix, ConfigDir, StateDir, Executable, Init, UCI, IP string }

func defaultSettings() Settings {
	return Settings{MDNS: true, HTTP: true, RaynetMode: "auto", AppMode: "auto", AxiomInterface: "br-lan", RemoteInterfaces: []string{"br-lan"}, DiscoveryMode: "auto", IP: "198.18.0.1", Prefix: 21, Serial: "AF002A4", Version: "V142.242.530", Hostname: "yachtsense-main", Instance: "yachtsense-main Settings", TTL: 120, HealthPort: 7777, LogLevel: "info", LogLines: 40}
}
func findProgram(paths ...string) string {
	for _, p := range paths {
		if filepath.IsAbs(p) {
			if s, e := os.Stat(p); e == nil && !s.IsDir() && s.Mode()&0111 != 0 {
				return p
			}
		} else if s, e := exec.LookPath(p); e == nil {
			return s
		}
	}
	return paths[len(paths)-1]
}
func discoverPaths() Paths {
	exe, _ := os.Executable()
	prefix := ""
	data, _ := os.ReadFile("/etc/opkg.conf")
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "dest" && f[1] == "root" {
			prefix = strings.TrimRight(f[2], "/")
			break
		}
	}
	// Prefer the configured logical root so USB/SD backing storage is transparent.
	candidates := []string{prefix, "/usr/local", ""}
	if strings.HasSuffix(exe, "/usr/sbin/"+serviceName) {
		candidates = append(candidates, strings.TrimSuffix(exe, "/usr/sbin/"+serviceName))
	}
	for _, p := range candidates {
		if _, e := os.Stat(p + "/etc/config/" + configName); e == nil {
			prefix = p
			break
		}
	}
	dir := prefix + "/etc/config"
	return Paths{Prefix: prefix, ConfigDir: dir, StateDir: "/var/run/" + serviceName, Executable: exe, Init: prefix + "/etc/init.d/" + serviceName, UCI: findProgram("/sbin/uci", "/usr/sbin/uci", "uci"), IP: findProgram("/sbin/ip", "/usr/sbin/ip", "/bin/ip", "ip")}
}
func boolText(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
func asBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "1", "on", "yes", "true", "enabled":
		return true, nil
	case "0", "off", "no", "false", "disabled":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", s)
}

// Parse UCI quoting without shell evaluation. This supports quotes, escaped
// quotes, inline comments and strings containing whitespace or shell syntax.
func words(line string) ([]string, error) {
	var out []string
	var b strings.Builder
	active := false
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if ch == '\\' && quote == '"' {
				if i+1 >= len(line) {
					return nil, errors.New("trailing escape")
				}
				i++
				b.WriteByte(line[i])
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		if ch == '#' && !active {
			break
		}
		switch ch {
		case '\'', '"':
			quote = ch
			active = true
		case '\\':
			if i+1 >= len(line) {
				return nil, errors.New("trailing escape")
			}
			i++
			b.WriteByte(line[i])
			active = true
		case ' ', '\t', '\r':
			if active {
				out = append(out, b.String())
				b.Reset()
				active = false
			}
		default:
			b.WriteByte(ch)
			active = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if active {
		out = append(out, b.String())
	}
	return out, nil
}
func parseUCI(data []byte) (map[string][]string, error) {
	out := map[string][]string{}
	active := false
	s := bufio.NewScanner(bytes.NewReader(data))
	s.Buffer(make([]byte, 4096), 65536)
	for s.Scan() {
		w, e := words(s.Text())
		if e != nil {
			return nil, e
		}
		if len(w) == 0 {
			continue
		}
		switch w[0] {
		case "config":
			active = len(w) >= 3 && w[1] == "emulator" && w[2] == "main"
		case "option", "list":
			if !active {
				continue
			}
			if len(w) != 3 {
				return nil, errors.New("invalid UCI option")
			}
			if w[0] == "option" {
				out[w[1]] = []string{w[2]}
			} else {
				out[w[1]] = append(out[w[1]], w[2])
			}
		}
	}
	return out, s.Err()
}
func settingsFrom(data []byte) (Settings, error) {
	c := defaultSettings()
	v, e := parseUCI(data)
	if e != nil {
		return c, e
	}
	get := func(k string) (string, bool) {
		a, ok := v[k]
		if !ok || len(a) == 0 {
			return "", false
		}
		return a[len(a)-1], true
	}
	stringsByKey := map[string]*string{"raynet_mode": &c.RaynetMode, "app_mode": &c.AppMode, "axiom_interface": &c.AxiomInterface, "discovery_mode": &c.DiscoveryMode, "ipaddr": &c.IP, "serial": &c.Serial, "version": &c.Version, "hostname": &c.Hostname, "instance": &c.Instance, "log_level": &c.LogLevel}
	for k, p := range stringsByKey {
		if s, ok := get(k); ok {
			*p = s
		}
	}
	for k, p := range map[string]*bool{"enabled": &c.Enabled, "mdns_enabled": &c.MDNS, "web_enabled": &c.HTTP, "manage_ip": &c.ManageIP, "remove_ip_on_stop": &c.RemoveIP} {
		if s, ok := get(k); ok {
			*p, e = asBool(s)
			if e != nil {
				return c, fmt.Errorf("%s: %w", k, e)
			}
		}
	}
	for k, p := range map[string]*int{"prefix": &c.Prefix, "ttl": &c.TTL, "health_port": &c.HealthPort, "log_lines": &c.LogLines} {
		if s, ok := get(k); ok {
			*p, e = strconv.Atoi(s)
			if e != nil {
				return c, fmt.Errorf("invalid %s", k)
			}
		}
	}
	if s, ok := v["remote_interface"]; ok {
		c.RemoteInterfaces = append([]string(nil), s...)
	}
	// Preserve an old explicit interface instead of inferring a new automatic mode.
	if _, ok := v["raynet_mode"]; !ok && len(v) > 0 {
		c.RaynetMode = "manual"
	}
	if _, ok := v["app_mode"]; !ok && len(v) > 0 {
		c.AppMode = "manual"
	}
	if _, ok := v["discovery_mode"]; !ok {
		switch s, _ := get("relay_mode"); s {
		case "force":
			c.DiscoveryMode = "builtin"
		case "disabled":
			c.DiscoveryMode = "disabled"
		}
	}
	return c, validateSettings(c)
}
func readSettings(p Paths) (Settings, error) {
	d, e := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	if e != nil {
		return defaultSettings(), fmt.Errorf("cannot read configuration: %w", e)
	}
	if len(d) > 65536 {
		return Settings{}, errors.New("configuration exceeds 64 KiB")
	}
	return settingsFrom(d)
}
func safeInterface(n string) bool {
	if len(n) < 1 || len(n) > 15 || n == "lo" {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			return false
		}
	}
	return true
}
func printable(s string, max int) bool {
	if len(s) < 1 || len(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validateSettings(c Settings) error {
	if c.RaynetMode != "auto" && c.RaynetMode != "manual" {
		return errors.New("invalid RayNet mode")
	}
	if c.AppMode != "auto" && c.AppMode != "manual" {
		return errors.New("invalid app-network mode")
	}
	switch c.DiscoveryMode {
	case "auto", "builtin", "avahi", "disabled":
	default:
		return errors.New("invalid discovery mode")
	}
	ip, e := netip.ParseAddr(c.IP)
	if e != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || c.IP == "255.255.255.255" {
		return errors.New("RayNet requires a unicast IPv4 address")
	}
	if c.Prefix < 0 || c.Prefix > 32 || c.HealthPort < 1 || c.HealthPort > 65535 || c.TTL < 1 || c.TTL > 86400 || c.LogLines < 5 || c.LogLines > 200 {
		return errors.New("prefix, port, TTL or log limit is out of range")
	}
	if c.RaynetMode == "manual" && !safeInterface(c.AxiomInterface) {
		return errors.New("invalid RayNet interface name")
	}
	if len(c.RemoteInterfaces) > 16 {
		return errors.New("at most 16 app networks are allowed")
	}
	if c.AppMode == "manual" && len(c.RemoteInterfaces) == 0 {
		return errors.New("choose an app network")
	}
	for _, n := range c.RemoteInterfaces {
		if !safeInterface(n) {
			return fmt.Errorf("invalid app interface %q", n)
		}
	}
	if !printable(c.Serial, 32) || c.Serial != cleanSerial(c.Serial) {
		return errors.New("serial must contain only letters, digits, - or _")
	}
	if !printable(c.Version, 64) || !printable(c.Instance, 63) || strings.Contains(c.Instance, ".") {
		return errors.New("invalid version or service instance (one DNS label, at most 63 bytes)")
	}
	if len(c.Hostname) < 1 || len(c.Hostname) > 63 || strings.HasPrefix(c.Hostname, "-") || strings.HasSuffix(c.Hostname, "-") {
		return errors.New("invalid hostname")
	}
	for _, r := range c.Hostname {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return errors.New("hostname must be a DNS label")
		}
	}
	if c.LogLevel != "info" && c.LogLevel != "debug" {
		return errors.New("invalid log level")
	}
	if c.ManageIP && c.RaynetMode != "manual" {
		return errors.New("address management requires a manually selected interface")
	}
	return nil
}
func fingerprint(c Settings) string {
	b, _ := json.Marshal(c)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func quoteUCI(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Execute child processes with a deadline and kill their process group as well.
// WaitDelay bounds inherited pipes: a grandchild cannot hold an API call open.
func runCommand(limit time.Duration, input []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	var out limitBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	e := cmd.Run()
	if ctx.Err() != nil {
		return out.Bytes(), fmt.Errorf("command timed out: %s", filepath.Base(name))
	}
	return out.Bytes(), e
}

type limitBuffer struct{ b bytes.Buffer }

func (b *limitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := 128*1024 - b.b.Len()
	if left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.b.Write(p)
	}
	return n, nil
}
func (b *limitBuffer) Bytes() []byte { return b.b.Bytes() }
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".ysle-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	if d, e := os.Open(dir); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
func writeSettings(p Paths, c Settings) error {
	if e := validateSettings(c); e != nil {
		return e
	}
	old, e := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	if e != nil {
		return e
	}
	stage, e := os.MkdirTemp(p.ConfigDir, ".ysle-stage-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if e = os.WriteFile(filepath.Join(stage, configName), old, 0600); e != nil {
		return e
	}
	if e = os.Mkdir(filepath.Join(stage, "delta"), 0700); e != nil {
		return e
	}
	values := map[string]string{"enabled": boolText(c.Enabled), "mdns_enabled": boolText(c.MDNS), "web_enabled": boolText(c.HTTP), "raynet_mode": c.RaynetMode, "app_mode": c.AppMode, "axiom_interface": c.AxiomInterface, "discovery_mode": c.DiscoveryMode, "manage_ip": boolText(c.ManageIP), "ipaddr": c.IP, "prefix": strconv.Itoa(c.Prefix), "remove_ip_on_stop": boolText(c.RemoveIP), "serial": c.Serial, "version": c.Version, "hostname": c.Hostname, "instance": c.Instance, "ttl": strconv.Itoa(c.TTL), "health_port": strconv.Itoa(c.HealthPort), "log_level": c.LogLevel, "log_lines": strconv.Itoa(c.LogLines)}
	var script strings.Builder
	fmt.Fprintf(&script, "set %s.main=emulator\n", configName)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&script, "set %s.main.%s=%s\n", configName, k, quoteUCI(values[k]))
	}
	// The option exists in every shipped configuration; normalize it before delete.
	fmt.Fprintf(&script, "set %s.main.remote_interface=''\ndelete %s.main.remote_interface\n", configName, configName)
	seen := map[string]bool{}
	for _, n := range c.RemoteInterfaces {
		if !seen[n] {
			fmt.Fprintf(&script, "add_list %s.main.remote_interface=%s\n", configName, quoteUCI(n))
			seen[n] = true
		}
	}
	fmt.Fprintf(&script, "commit %s\n", configName)
	out, e := runCommand(5*time.Second, []byte(script.String()), p.UCI, "-c", stage, "-t", filepath.Join(stage, "delta"), "batch")
	if e != nil {
		return fmt.Errorf("UCI save failed: %v %s", e, strings.TrimSpace(string(out)))
	}
	data, e := os.ReadFile(filepath.Join(stage, configName))
	if e != nil {
		return e
	}
	saved, e := settingsFrom(data)
	if e != nil || fingerprint(saved) != fingerprint(c) {
		return errors.New("UCI round-trip verification failed; existing settings were not replaced")
	}
	if e = atomicWrite(filepath.Join(p.ConfigDir, "."+configName+".previous"), old, 0600); e != nil {
		return e
	}
	return atomicWrite(filepath.Join(p.ConfigDir, configName), data, 0600)
}

type InterfaceInfo struct {
	Name         string   `json:"name"`
	Addresses    []string `json:"addresses"`
	MAC          string   `json:"mac"`
	Up           bool     `json:"up"`
	DefaultRoute bool     `json:"default_route"`
	ReadError    string   `json:"read_error,omitempty"`
}

func interfaceSnapshot() ([]InterfaceInfo, error) {
	list, e := net.Interfaces()
	if e != nil {
		return nil, e
	}
	defaults := map[string]bool{}
	d, _ := os.ReadFile("/proc/net/route")
	for _, l := range strings.Split(string(d), "\n") {
		f := strings.Fields(l)
		if len(f) > 3 && f[1] == "00000000" {
			defaults[f[0]] = true
		}
	}
	result := []InterfaceInfo{}
	for _, i := range list {
		if i.Flags&net.FlagLoopback != 0 || !safeInterface(i.Name) {
			continue
		}
		item := InterfaceInfo{Name: i.Name, Addresses: []string{}, MAC: i.HardwareAddr.String(), Up: i.Flags&net.FlagUp != 0, DefaultRoute: defaults[i.Name]}
		addrs, err := i.Addrs()
		if err != nil {
			item.ReadError = err.Error()
		} else {
			for _, a := range addrs {
				p, err := netip.ParsePrefix(a.String())
				if err == nil && p.Addr().Is4() {
					item.Addresses = append(item.Addresses, a.String())
				}
			}
		}
		sort.Strings(item.Addresses)
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

type Resolved struct {
	Raynet    string   `json:"raynet"`
	CIDR      string   `json:"cidr"`
	Apps      []string `json:"apps"`
	Error     string   `json:"error,omitempty"`
	Reason    string   `json:"reason"`
	AppReason string   `json:"app_reason"`
}

func hasIP(i InterfaceInfo, ip string) string {
	for _, cidr := range i.Addresses {
		p, e := netip.ParsePrefix(cidr)
		if e == nil && p.Addr().String() == ip {
			return cidr
		}
	}
	return ""
}
func resolve(c Settings, ifs []InterfaceInfo) (Resolved, error) {
	r := Resolved{Apps: []string{}}
	by := map[string]InterfaceInfo{}
	for _, i := range ifs {
		by[i.Name] = i
	}
	fail := func(s string) (Resolved, error) { r.Error = s; return r, errors.New(s) }
	if c.RaynetMode == "auto" {
		matches := []InterfaceInfo{}
		for _, i := range ifs {
			if hasIP(i, c.IP) != "" {
				matches = append(matches, i)
			}
		}
		if len(matches) == 0 {
			return fail("RayNet address " + c.IP + " was not found; configure it in RutOS or choose address management under Advanced")
		}
		if len(matches) > 1 {
			return fail("RayNet address exists on multiple interfaces; choose one manually")
		}
		r.Raynet = matches[0].Name
		r.CIDR = hasIP(matches[0], c.IP)
		r.Reason = "Detected from the existing IPv4 address"
	} else {
		i, ok := by[c.AxiomInterface]
		if !ok {
			return fail("Selected RayNet interface " + c.AxiomInterface + " is unavailable")
		}
		r.Raynet = i.Name
		r.CIDR = hasIP(i, c.IP)
		r.Reason = "Manually selected"
		if r.CIDR == "" {
			if !c.ManageIP {
				return fail("RayNet address " + c.IP + " is absent on " + i.Name)
			}
			r.CIDR = c.IP + "/" + strconv.Itoa(c.Prefix)
			r.Reason = "Explicit address management"
		}
	}
	if i := by[r.Raynet]; !i.Up || i.ReadError != "" {
		return fail("RayNet interface " + r.Raynet + " is down or could not be read")
	}
	if c.AppMode == "manual" {
		r.Apps = append(r.Apps, c.RemoteInterfaces...)
		r.AppReason = "Manually selected"
	} else {
		suitable := func(i InterfaceInfo) bool {
			if !i.Up || i.ReadError != "" || i.DefaultRoute {
				return false
			}
			for _, a := range i.Addresses {
				p, e := netip.ParsePrefix(a)
				if e == nil && p.Addr().IsPrivate() {
					return true
				}
			}
			return false
		}
		if i, ok := by["br-lan"]; ok && (suitable(i) || i.Name == r.Raynet && i.Up && i.ReadError == "") {
			r.Apps = []string{i.Name}
			r.AppReason = "LAN bridge with a private IPv4 address"
		} else {
			candidates := []string{}
			for _, i := range ifs {
				if suitable(i) {
					candidates = append(candidates, i.Name)
				}
			}
			if len(candidates) != 1 {
				return fail("No unambiguous app network; choose the phone/tablet network manually")
			}
			r.Apps = candidates
			r.AppReason = "Only eligible private IPv4 network"
		}
	}
	seen := map[string]bool{}
	apps := []string{}
	for _, n := range r.Apps {
		if seen[n] {
			continue
		}
		seen[n] = true
		i, ok := by[n]
		if !ok || !i.Up || i.ReadError != "" {
			return fail("App interface " + n + " is unavailable")
		}
		if len(i.Addresses) == 0 && n != r.Raynet {
			return fail("App interface " + n + " has no IPv4 address")
		}
		apps = append(apps, n)
	}
	r.Apps = apps
	if len(apps) == 0 {
		return fail("Choose an app network")
	}
	return r, nil
}

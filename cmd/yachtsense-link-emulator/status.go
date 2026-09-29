package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type AvahiInfo struct {
	Running   bool     `json:"running"`
	Reflector bool     `json:"reflector"`
	Eligible  bool     `json:"eligible"`
	Unknown   bool     `json:"unknown"`
	Config    string   `json:"config"`
	Reason    string   `json:"reason"`
	UMDNS     bool     `json:"umdns"`
	Allow     []string `json:"allow"`
	Deny      []string `json:"deny"`
}

func processIDs(name string) []int {
	entries, _ := os.ReadDir("/proc")
	out := []int{}
	for _, ent := range entries {
		pid, e := strconv.Atoi(ent.Name())
		if e != nil {
			continue
		}
		comm, e := os.ReadFile(filepath.Join("/proc", ent.Name(), "comm"))
		if e == nil && strings.TrimSpace(string(comm)) == name {
			out = append(out, pid)
		}
	}
	return out
}
func parseINI(data string) map[string]string {
	out := map[string]string{}
	section := ""
	for _, l := range strings.Split(data, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			section = strings.Trim(l, "[]")
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if ok {
			out[section+"."+strings.TrimSpace(k)] = strings.TrimSpace(strings.SplitN(v, "#", 2)[0])
		}
	}
	return out
}
func commaList(v string) []string {
	out := []string{}
	for _, s := range strings.Split(v, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func avahiFrom(data string, r Resolved) AvahiInfo {
	v := parseINI(data)
	a := AvahiInfo{Running: true, Allow: commaList(v["server.allow-interfaces"]), Deny: commaList(v["server.deny-interfaces"])}
	a.Reflector, _ = asBool(v["reflector.enable-reflector"])
	if !a.Reflector {
		a.Reason = "Avahi is running as a responder, not a reflector"
		return a
	}
	if v["server.use-ipv4"] == "no" {
		a.Reason = "Avahi reflection is configured but IPv4 is disabled"
		return a
	}
	for _, n := range append([]string{r.Raynet}, r.Apps...) {
		if n == "" {
			a.Reason = "Choose networks before checking Avahi coverage"
			return a
		}
		if contains(a.Deny, n) || len(a.Allow) > 0 && !contains(a.Allow, n) {
			a.Reason = "Avahi interface restrictions do not cover " + n
			return a
		}
	}
	// Service filters make packet coverage uncertain, even with the right NICs.
	if v["reflector.reflect-filters"] != "" {
		a.Reason = "Avahi has reflection filters; Raymarine coverage needs verification"
		return a
	}
	a.Eligible = true
	a.Reason = "Running process and matching reflector configuration found; packet forwarding is not independently verified"
	return a
}
func inspectAvahi(p Paths, r Resolved) AvahiInfo {
	pids := processIDs("avahi-daemon")
	a := AvahiInfo{UMDNS: len(processIDs("umdns")) > 0, Reason: "No Avahi process detected", Allow: []string{}, Deny: []string{}}
	if len(pids) == 0 {
		return a
	}
	a.Running = true
	paths := []string{}
	for _, pid := range pids {
		b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		args := strings.Split(string(b), "\x00")
		for i, s := range args {
			if (s == "-f" || s == "--file") && i+1 < len(args) {
				paths = append(paths, args[i+1])
			}
			if strings.HasPrefix(s, "--file=") {
				paths = append(paths, strings.TrimPrefix(s, "--file="))
			}
		}
	}
	paths = append(paths, "/etc/avahi/avahi-daemon.conf", p.Prefix+"/etc/avahi/avahi-daemon.conf")
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e == nil {
			out := avahiFrom(string(b), r)
			out.Config = path
			out.UMDNS = a.UMDNS
			return out
		}
	}
	a.Unknown = true
	a.Reason = "Avahi is running but its configuration cannot be read"
	return a
}
func chooseEngine(c Settings, r Resolved, a AvahiInfo) (string, bool, string, error) {
	distinct := false
	for _, n := range r.Apps {
		if n != r.Raynet {
			distinct = true
		}
	}
	if !distinct {
		return "direct", false, "Both roles share one interface; no cross-network relay is needed", nil
	}
	switch c.DiscoveryMode {
	case "disabled":
		return "disabled", false, "Cross-network relay was explicitly disabled", nil
	case "avahi":
		if !a.Eligible {
			return "avahi", false, a.Reason, fmt.Errorf("Avahi cannot be selected: %s", a.Reason)
		}
		return "avahi", false, a.Reason, nil
	case "builtin":
		if a.Running && (a.Reflector || a.Unknown) {
			return "conflict", false, "An existing or unverified reflector is already present", fmt.Errorf("disable the overlapping Avahi reflector before selecting the built-in relay; no Avahi settings were changed")
		}
		return "builtin", true, "Built-in relay explicitly selected", nil
	default:
		if a.Eligible {
			return "avahi", false, a.Reason, nil
		}
		if a.Running && (a.Reflector || a.Unknown) {
			return "conflict", false, a.Reason, fmt.Errorf("automatic discovery needs attention: %s", a.Reason)
		}
		return "builtin", true, "No existing reflector for these networks; using the built-in relay", nil
	}
}
func currentStatus(p Paths, diagnostics bool) map[string]any {
	c, ce := readSettings(p)
	ifs, ie := interfaceSnapshot()
	r, re := resolve(c, ifs)
	a := inspectAvahi(p, r)
	v := readRuntime(p)
	running := processMatches(v, p.Executable)
	now := time.Now().UTC()
	stale := !running || now.Sub(v.Updated) > 20*time.Second
	mode, _, why, ee := chooseEngine(c, r, a)
	errorsList := []string{}
	if ce != nil {
		errorsList = append(errorsList, ce.Error())
	}
	if ie != nil {
		errorsList = append(errorsList, ie.Error())
	}
	if re != nil {
		errorsList = append(errorsList, re.Error())
	}
	if ee != nil {
		errorsList = append(errorsList, ee.Error())
	}
	action := readAction(p)
	if (action.State == "queued" || action.State == "applying") && now.Sub(action.Updated) > 65*time.Second {
		action.State = "failed"
		action.Message = "Operation did not finish within its time limit; inspect Diagnostics"
	}
	freshAxiom := !stale && v.Axiom.IP != "" && now.Before(v.Axiom.Expires)
	freshApp := !stale && v.App.IP != "" && now.Before(v.App.Expires)
	pendingConfig := running && v.ConfigHash != fingerprint(c)
	st := map[string]any{"running": running, "stale": stale, "config_pending": pendingConfig, "mdns_active": !stale && v.MDNS, "http_active": !stale && v.HTTP, "relay_active": !stale && v.Relay, "axiom_detected": freshAxiom, "app_detected": freshApp, "configured_engine": mode, "configured_reason": why, "runtime": v, "errors": errorsList, "observed_at": now, "package_version": packageVersion}
	out := map[string]any{"ok": true, "config": c, "interfaces": ifs, "snapshot_ok": ie == nil, "networks": r, "avahi": a, "status": st, "action": action}
	if diagnostics {
		logs, e := runCommand(3*time.Second, nil, findProgram("/sbin/logread", "logread"), "-e", serviceName)
		if e != nil && len(logs) == 0 {
			logs = []byte("logread unavailable: " + e.Error())
		}
		lines := strings.Split(strings.TrimSpace(string(logs)), "\n")
		if len(lines) > c.LogLines {
			lines = lines[len(lines)-c.LogLines:]
		}
		actionLog, _ := os.ReadFile(filepath.Join(p.StateDir, "action.log"))
		if len(actionLog) > 32768 {
			actionLog = actionLog[len(actionLog)-32768:]
		}
		listeners := []string{}
		if b, e := runCommand(2*time.Second, nil, findProgram("ss", "netstat"), "-lnup"); e == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.Contains(l, ":5353") || strings.Contains(l, ".5353") {
					listeners = append(listeners, l)
				}
			}
		}
		fw, _ := os.ReadFile("/etc/version")
		out["diagnostics"] = map[string]any{"config_dir": p.ConfigDir, "executable": p.Executable, "firmware": strings.TrimSpace(string(fw)), "listeners": listeners, "logs": lines, "action_log": string(actionLog), "discovery_note": "The daemon always provides YachtSense identity and HTTP. Avahi, when selected, only supplies cross-network mDNS reflection. Neither proves internet access or app connectivity."}
	}
	return out
}

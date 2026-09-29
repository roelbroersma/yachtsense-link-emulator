package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixtures() []InterfaceInfo {
	return []InterfaceInfo{{Name: "eth0.3", Up: true, Addresses: []string{"198.18.0.1/21"}}, {Name: "br-lan", Up: true, Addresses: []string{"192.168.40.1/24"}}, {Name: "wwan0", Up: true, DefaultRoute: true, Addresses: []string{"10.1.1.1/30"}}}
}
func TestAutomaticNetworks(t *testing.T) {
	c := defaultSettings()
	r, e := resolve(c, fixtures())
	if e != nil || r.Raynet != "eth0.3" || !reflect.DeepEqual(r.Apps, []string{"br-lan"}) {
		t.Fatal(r, e)
	}
}
func TestManualChoicePersists(t *testing.T) {
	c := defaultSettings()
	c.RaynetMode = "manual"
	c.AxiomInterface = "eth0.3"
	c.AppMode = "manual"
	c.RemoteInterfaces = []string{"wwan0"}
	r, e := resolve(c, fixtures())
	if e != nil || r.Apps[0] != "wwan0" {
		t.Fatal(r, e)
	}
}
func TestSameInterfaceAllowed(t *testing.T) {
	c := defaultSettings()
	ifs := []InterfaceInfo{{Name: "br-lan", Up: true, Addresses: []string{"198.18.0.1/21", "192.168.40.1/24"}}}
	r, e := resolve(c, ifs)
	if e != nil || r.Raynet != "br-lan" || r.Apps[0] != "br-lan" {
		t.Fatal(r, e)
	}
	m, relay, _, e := chooseEngine(c, r, AvahiInfo{})
	if e != nil || relay || m != "direct" {
		t.Fatal(m, e)
	}
}
func TestNoWANAutoselection(t *testing.T) {
	ifs := fixtures()
	ifs = append(ifs[:1], ifs[2])
	if _, e := resolve(defaultSettings(), ifs); e == nil {
		t.Fatal("WAN chosen automatically")
	}
}
func TestAmbiguousRaynet(t *testing.T) {
	ifs := fixtures()
	ifs = append(ifs, InterfaceInfo{Name: "eth0.5", Up: true, Addresses: []string{"198.18.0.1/24"}})
	if _, e := resolve(defaultSettings(), ifs); e == nil {
		t.Fatal("ambiguous IP silently selected")
	}
}
func TestExistingPrefixIsObserved(t *testing.T) {
	ifs := fixtures()
	ifs[0].Addresses = []string{"198.18.0.1/24"}
	r, e := resolve(defaultSettings(), ifs)
	if e != nil || r.CIDR != "198.18.0.1/24" {
		t.Fatal(r, e)
	}
}
func TestExplicitManagementOnly(t *testing.T) {
	c := defaultSettings()
	c.ManageIP = true
	if validateSettings(c) == nil {
		t.Fatal("management allowed in automatic mode")
	}
	c.RaynetMode = "manual"
	c.AxiomInterface = "eth0.3"
	ifs := fixtures()
	ifs[0].Addresses = nil
	r, e := resolve(c, ifs)
	if e != nil || r.CIDR != "198.18.0.1/21" {
		t.Fatal(r, e)
	}
}
func TestAvahiChoices(t *testing.T) {
	c := defaultSettings()
	r, _ := resolve(c, fixtures())
	for _, tt := range []struct {
		name, ini, mode, want string
		bad                   bool
	}{{"responder", "[reflector]\nenable-reflector=no", "auto", "builtin", false}, {"reflector", "[reflector]\nenable-reflector=yes", "auto", "avahi", false}, {"restricted", "[server]\nallow-interfaces=br-lan\n[reflector]\nenable-reflector=yes", "auto", "conflict", true}, {"no duplicate relay", "[reflector]\nenable-reflector=yes", "builtin", "conflict", true}, {"not installed", "", "avahi", "avahi", true}, {"filtered", "[reflector]\nenable-reflector=yes\nreflect-filters=_airplay._tcp.local", "auto", "conflict", true}} {
		t.Run(tt.name, func(t *testing.T) {
			a := avahiFrom(tt.ini, r)
			c.DiscoveryMode = tt.mode
			m, _, _, e := chooseEngine(c, r, a)
			if m != tt.want || (e != nil) != tt.bad {
				t.Fatal(m, e)
			}
		})
	}
}
func TestUCIQuotes(t *testing.T) {
	for _, s := range []string{"normal", "a b", "a'b", "$(touch /tmp/never)", "semi;colon", "back\\slash", "has#hash", "has\"quote"} {
		w, e := words("option version " + quoteUCI(s))
		if e != nil || len(w) != 3 || w[2] != s {
			t.Fatalf("%q: %v %v", s, w, e)
		}
	}
}
func TestLegacyMigrationPreservesChoices(t *testing.T) {
	b := []byte("config emulator 'main'\n option enabled '1'\n option axiom_interface 'eth0.3'\n list remote_interface 'br-lan'\n option manage_ip '0'\n option relay_mode 'force'\n")
	c, e := settingsFrom(b)
	if e != nil || c.RaynetMode != "manual" || c.AppMode != "manual" || c.DiscoveryMode != "builtin" || c.ManageIP {
		t.Fatal(c, e)
	}
}
func TestValidateRejectsInvalidPayload(t *testing.T) {
	for _, mutate := range []func(*Settings){func(c *Settings) { c.Prefix = 33 }, func(c *Settings) { c.IP = "127.0.0.1" }, func(c *Settings) { c.Hostname = "a;reboot" }, func(c *Settings) { c.RemoteInterfaces = []string{"$(reboot)"} }, func(c *Settings) { c.Instance = "a\nb" }, func(c *Settings) { c.TTL = 0 }, func(c *Settings) { c.Serial = " " }} {
		c := defaultSettings()
		mutate(&c)
		if validateSettings(c) == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	if _, e := parsePayload([]byte(`{"unknown":1}`)); e == nil {
		t.Fatal("unknown option accepted")
	}
	if _, e := parsePayload([]byte(`{} {}`)); e == nil {
		t.Fatal("trailing JSON accepted")
	}
}
func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	data, e := os.ReadFile("../../package/root/etc/config/" + configName)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, configName), data, 0600); e != nil {
		t.Fatal(e)
	}
	uci, _ := filepath.Abs("../../tests/fake_uci.py")
	return Paths{ConfigDir: dir, StateDir: filepath.Join(dir, "state"), UCI: uci}
}
func TestAtomicUCITransaction(t *testing.T) {
	p := testPaths(t)
	c, _ := readSettings(p)
	c.RaynetMode = "manual"
	c.AxiomInterface = "eth0.3"
	c.AppMode = "manual"
	c.RemoteInterfaces = []string{"br-lan", "eth0.3"}
	c.Version = "Test's $(echo literal)"
	if e := writeSettings(p, c); e != nil {
		t.Fatal(e)
	}
	got, e := readSettings(p)
	if e != nil || !reflect.DeepEqual(got, c) {
		t.Fatal(got, e)
	}
	if _, e = os.Stat(filepath.Join(p.ConfigDir, "."+configName+".previous")); e != nil {
		t.Fatal("backup missing", e)
	}
}
func TestUCIFailureLeavesOriginal(t *testing.T) {
	for _, script := range []string{"#!/bin/sh\nexit 1\n", "#!/bin/sh\nexit 0\n"} {
		p := testPaths(t)
		original, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
		fake := filepath.Join(p.ConfigDir, "uci")
		os.WriteFile(fake, []byte(script), 0700)
		p.UCI = fake
		c, _ := readSettings(p)
		c.Enabled = true
		if e := writeSettings(p, c); e == nil {
			t.Fatal("failed UCI accepted")
		}
		now, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
		if !bytes.Equal(original, now) {
			t.Fatal("failed transaction altered original")
		}
	}
}
func TestCommandTimeout(t *testing.T) {
	start := time.Now()
	_, e := runCommand(150*time.Millisecond, nil, "/bin/sh", "-c", "trap '' TERM; sleep 30 & wait")
	if e == nil || time.Since(start) > 2*time.Second {
		t.Fatal("unbounded child process", e)
	}
}
func TestActionLockAndRelease(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	a, e := lockFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := lockFile(p); e == nil {
		b.Close()
		t.Fatal("concurrent action accepted")
	}
	a.Close()
	b, e := lockFile(p)
	if e != nil {
		t.Fatal(e)
	}
	b.Close()
}
func TestAtomicRuntimeAndPID(t *testing.T) {
	p := testPaths(t)
	os.Mkdir(p.StateDir, 0700)
	s := &stateStore{path: filepath.Join(p.StateDir, "runtime.json"), v: Runtime{PID: os.Getpid(), ProcStart: processStart(os.Getpid())}}
	s.update(func(v *Runtime) { v.Raynet = "eth0.3" })
	s.flush()
	r := readRuntime(p)
	exe, _ := os.Executable()
	if !processMatches(r, exe) {
		t.Fatal("active PID not recognized")
	}
	r.ProcStart = "wrong"
	if processMatches(r, exe) {
		t.Fatal("recycled PID accepted")
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), "eth0.3") {
		t.Fatal("state missing")
	}
}
func TestStatusDoesNotModifyConfiguration(t *testing.T) {
	p := testPaths(t)
	before, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	_ = currentStatus(p, false)
	after, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	if !bytes.Equal(before, after) {
		t.Fatal("read-only status wrote configuration")
	}
	if _, e := os.Stat(p.StateDir); !os.IsNotExist(e) {
		t.Fatal("status created runtime directory")
	}
}
func TestInheritedWorkerLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l")
	f, e := lockFile(p)
	if e != nil {
		t.Fatal(e)
	}
	fd, e := syscall.Dup(int(f.Fd()))
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if other, e := lockFile(p); e == nil {
		other.Close()
		t.Fatal("duplicate did not preserve lock")
	}
	syscall.Close(fd)
	other, e := lockFile(p)
	if e != nil {
		t.Fatal(e)
	}
	other.Close()
}

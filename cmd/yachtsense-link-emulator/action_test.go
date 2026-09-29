package main

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActionFSMStop(t *testing.T) {
	p := testPaths(t)
	os.MkdirAll(p.StateDir, 0750)
	p.Init = filepath.Join(p.ConfigDir, "init")
	calls := filepath.Join(p.StateDir, "calls")
	os.WriteFile(p.Init, []byte("#!/bin/sh\necho \"$1\" >> "+quoteUCI(calls)+"\n"), 0700)
	if e := applyAction(p, "stop", "test-stop"); e != nil {
		t.Fatal(e)
	}
	a := readAction(p)
	b, _ := os.ReadFile(calls)
	if a.State != "done" || string(b) != "stop\ndisable\n" {
		t.Fatal(a, string(b))
	}
}
func TestActionFSMFailedStep(t *testing.T) {
	p := testPaths(t)
	os.MkdirAll(p.StateDir, 0750)
	p.Init = filepath.Join(p.ConfigDir, "init")
	os.WriteFile(p.Init, []byte("#!/bin/sh\necho deliberate-error\nexit 4\n"), 0700)
	if e := applyAction(p, "stop", "test-fail"); e == nil {
		t.Fatal("failed init accepted")
	}
	a := readAction(p)
	if a.State != "failed" || !strings.Contains(a.Message, "deliberate-error") {
		t.Fatal(a)
	}
}
func TestActionFSMReadiness(t *testing.T) {
	p := testPaths(t)
	os.MkdirAll(p.StateDir, 0750)
	c, _ := readSettings(p)
	c.Enabled = true
	if e := writeSettings(p, c); e != nil {
		t.Fatal(e)
	}
	p.Executable, _ = os.Executable()
	p.Init = filepath.Join(p.ConfigDir, "init")
	v := Runtime{PID: os.Getpid(), ProcStart: processStart(os.Getpid()), ConfigHash: fingerprint(c), MDNS: true, HTTP: true, Updated: time.Now()}
	b, _ := json.Marshal(v)
	script := "#!/bin/sh\ncase \"$1\" in\nstop) rm -f " + quoteUCI(filepath.Join(p.StateDir, "runtime.json")) + " ;;\nstart) printf '%s' " + quoteUCI(string(b)) + " > " + quoteUCI(filepath.Join(p.StateDir, "runtime.json")) + " ;;\nesac\nexit 0\n"
	os.WriteFile(p.Init, []byte(script), 0700)
	if e := applyAction(p, "restart", "ready"); e != nil {
		t.Fatal(e)
	}
	if a := readAction(p); a.State != "done" {
		t.Fatal(a)
	}
}
func TestOwnAddressRelinquishedBeforeStop(t *testing.T) {
	ifs, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	var name, cidr string
	for _, i := range ifs {
		as, _ := i.Addrs()
		for _, a := range as {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil && ip.To4() != nil && !ip.IsLoopback() {
				name = i.Name
				cidr = a.String()
				break
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		t.Skip("no non-loopback IPv4 interface available")
	}
	p := testPaths(t)
	os.MkdirAll(p.StateDir, 0750)
	sentinel := filepath.Join(p.StateDir, "ip-was-called")
	p.IP = filepath.Join(p.ConfigDir, "ip")
	os.WriteFile(p.IP, []byte("#!/bin/sh\ntouch "+quoteUCI(sentinel)+"\nexit 0\n"), 0700)
	b, _ := json.Marshal(managedAddress{name, cidr})
	marker := filepath.Join(p.StateDir, "managed-ip.json")
	os.WriteFile(marker, b, 0600)
	c := defaultSettings()
	c.IP = strings.Split(cidr, "/")[0]
	c.ManageIP = true
	c.RemoveIP = true
	cleanup, e := prepareAddress(p, c, Resolved{Raynet: name, CIDR: cidr})
	if e != nil {
		t.Fatal(e)
	}
	os.Remove(marker)
	cleanup()
	if _, e := os.Stat(sentinel); e == nil {
		t.Fatal("cleanup removed a relinquished address")
	}
}
func TestQuietRuntimeHeartbeat(t *testing.T) {
	s := &stateStore{v: Runtime{Updated: time.Now().Add(-time.Hour)}}
	s.update(func(v *Runtime) {})
	if time.Since(s.snapshot().Updated) > time.Second {
		t.Fatal("quiet daemon has stale heartbeat")
	}
}
func TestMalformedPayloadDoesNotWrite(t *testing.T) {
	p := testPaths(t)
	before, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	_, e := requestAction(p, "save", []byte(`{"prefix":99}`))
	if e == nil {
		t.Fatal("invalid write accepted")
	}
	after, _ := os.ReadFile(filepath.Join(p.ConfigDir, configName))
	if !bytes.Equal(before, after) {
		t.Fatal("configuration changed")
	}
}

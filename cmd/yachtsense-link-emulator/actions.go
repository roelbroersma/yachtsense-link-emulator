package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Action struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	State   string    `json:"state"`
	Message string    `json:"message"`
	Updated time.Time `json:"updated"`
}

func readAction(p Paths) Action {
	var a Action
	b, e := os.ReadFile(filepath.Join(p.StateDir, "action.json"))
	if e == nil && len(b) < 65536 {
		_ = json.Unmarshal(b, &a)
	}
	return a
}
func storeAction(p Paths, a Action) error {
	a.Updated = time.Now().UTC()
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(p.StateDir, "action.json"), b, 0640)
}
func parsePayload(b []byte) (Settings, error) {
	c := defaultSettings()
	if len(b) > 16384 {
		return c, errors.New("payload exceeds 16 KiB")
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	var more any
	if e := d.Decode(&more); e != io.EOF {
		return c, errors.New("unexpected trailing payload")
	}
	seen := map[string]bool{}
	a := []string{}
	for _, n := range c.RemoteInterfaces {
		if !seen[n] {
			seen[n] = true
			a = append(a, n)
		}
	}
	c.RemoteInterfaces = a
	return c, validateSettings(c)
}
func requestAction(p Paths, kind string, input []byte) (map[string]any, error) {
	if e := os.MkdirAll(p.StateDir, 0750); e != nil {
		return nil, e
	}
	f, e := lockFile(filepath.Join(p.StateDir, "action.lock"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	old, e := readSettings(p)
	if e != nil {
		return nil, e
	}
	c := old
	switch kind {
	case "save":
		c, e = parsePayload(input)
		if e != nil {
			return nil, e
		}
	case "start":
		c.Enabled = true
	case "stop":
		c.Enabled = false
	case "restart":
		if !c.Enabled {
			return nil, errors.New("service is disabled; turn it on before restarting")
		}
	default:
		return nil, errors.New("unknown action")
	}
	if c.Enabled {
		ifs, e := interfaceSnapshot()
		if e != nil {
			return nil, e
		}
		r, e := resolve(c, ifs)
		if e != nil {
			return nil, e
		}
		if _, _, _, e = chooseEngine(c, r, inspectAvahi(p, r)); e != nil {
			return nil, e
		}
	}
	if s, e := os.Stat(p.Init); e != nil || s.Mode()&0111 == 0 {
		return nil, errors.New("package init script is missing or not executable")
	}
	changed := fingerprint(c) != fingerprint(old)
	if changed {
		if e = writeSettings(p, c); e != nil {
			return nil, e
		}
	}
	if !c.ManageIP && changed {
		// Configuration is committed first; relinquishing ownership must not
		// happen on a validation or write failure.
		_ = os.Remove(filepath.Join(p.StateDir, "managed-ip.json"))
	}
	raw := make([]byte, 12)
	if _, e = rand.Read(raw); e != nil {
		return nil, e
	}
	id := hex.EncodeToString(raw)
	a := Action{ID: id, Kind: kind, State: "queued", Message: "Settings saved; applying service state"}
	if e = storeAction(p, a); e != nil {
		return nil, e
	}
	output, e := os.OpenFile(filepath.Join(p.StateDir, "action.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return nil, e
	}
	defer output.Close()
	cmd := exec.Command(p.Executable, "--worker", kind, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = output
	cmd.Stderr = output
	// Share the open-file-description lock with the worker. Closing our duplicate
	// does not release the lock while the worker is applying this configuration.
	cmd.ExtraFiles = []*os.File{f}
	if e = cmd.Start(); e != nil {
		a.State = "failed"
		a.Message = "Configuration saved, but worker could not start: " + e.Error()
		_ = storeAction(p, a)
		return nil, errors.New(a.Message)
	}
	_ = cmd.Process.Release()
	return map[string]any{"ok": true, "saved": true, "action_id": id, "message": "Saved; service operation queued"}, nil
}
func worker(p Paths, kind, id string) error {
	inherited := os.NewFile(3, "action-lock")
	if inherited == nil {
		return errors.New("missing worker lock")
	}
	defer inherited.Close()
	return applyAction(p, kind, id)
}

// Separate the transition state machine from descriptor inheritance for tests.
func applyAction(p Paths, kind, id string) error {
	a := Action{ID: id, Kind: kind, State: "applying", Message: "Applying service state"}
	_ = storeAction(p, a)
	fail := func(e error) error { a.State = "failed"; a.Message = e.Error(); _ = storeAction(p, a); return e }
	c, e := readSettings(p)
	if e != nil {
		return fail(e)
	}
	step := func(action string) error {
		a.Message = "Service: " + action
		_ = storeAction(p, a)
		fmt.Println(a.Message)
		b, e := runCommand(12*time.Second, nil, p.Init, action)
		fmt.Print(string(b))
		if e != nil {
			return fmt.Errorf("%s failed: %v %s", action, e, strings.TrimSpace(string(b)))
		}
		return nil
	}
	// Stop and start are distinct commands; rc.common restart ignores SIGTERM on
	// some RutOS versions. Each subprocess group has its own hard deadline.
	if kind != "start" {
		if e = step("stop"); e != nil {
			return fail(e)
		}
		until := time.Now().Add(7 * time.Second)
		for processMatches(readRuntime(p), p.Executable) {
			if time.Now().After(until) {
				return fail(errors.New("old daemon did not stop"))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if c.Enabled {
		if e = step("enable"); e != nil {
			return fail(e)
		}
		if e = step("start"); e != nil {
			return fail(e)
		}
		until := time.Now().Add(15 * time.Second)
		stable := 0
		pid := 0
		for time.Now().Before(until) {
			v := readRuntime(p)
			ready := processMatches(v, p.Executable) && v.ConfigHash == fingerprint(c) && (!c.MDNS || v.MDNS) && (!c.HTTP || v.HTTP)
			if ready {
				if pid == v.PID {
					stable++
				} else {
					stable = 1
					pid = v.PID
				}
				if stable >= 3 {
					a.State = "done"
					a.Message = "Service is running with the saved settings"
					return storeAction(p, a)
				}
			} else {
				stable = 0
			}
			time.Sleep(time.Second)
		}
		return fail(errors.New("settings saved, but service readiness was not confirmed; see daemon logs"))
	}
	if e = step("disable"); e != nil {
		return fail(e)
	}
	a.State = "done"
	a.Message = "Service stopped"
	return storeAction(p, a)
}

package main

import (
	"context"
	"embed"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard/index.html dashboard/status.css dashboard/status.js
var dashboardFiles embed.FS

// The public server only serves immutable assets and a typed, cached snapshot.
// It has no RPC passthrough, configuration accessor or mutation endpoint.
type dashboardCache struct {
	mu    sync.RWMutex
	view  RouterView
	ready bool
}

func (d *dashboardCache) replace(v RouterView) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.view = v
	d.ready = true
}
func (d *dashboardCache) snapshot() (RouterView, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.view, d.ready
}
func dashboardNetworks(c Settings, r Resolved) []netip.Prefix {
	out := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	if p, e := netip.ParsePrefix(r.CIDR); e == nil {
		out = append(out, p.Masked())
	}
	for _, name := range r.Apps {
		nic, e := net.InterfaceByName(name)
		if e != nil {
			continue
		}
		addrs, e := nic.Addrs()
		if e != nil {
			continue
		}
		for _, a := range addrs {
			if p, e := netip.ParsePrefix(a.String()); e == nil && p.Addr().Is4() {
				out = append(out, p.Masked())
			}
		}
	}
	for _, s := range strings.FieldsFunc(c.DashboardCIDRs, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if p, e := netip.ParsePrefix(s); e == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}
func dashboardAllowed(remote string, all bool, prefixes []netip.Prefix) bool {
	host, _, e := net.SplitHostPort(remote)
	if e != nil {
		return false
	}
	a, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	a = a.Unmap()
	if all {
		return true
	}
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
func dashboardHandler(c Settings, r Resolved, state *stateStore, cache *dashboardCache) http.Handler {
	prefixes := dashboardNetworks(c, r)
	assets := map[string]string{"/": "index.html", "/index.html": "index.html", "/status.css": "status.css", "/status.js": "status.js"}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'")
		if !dashboardAllowed(req.RemoteAddr, c.DashboardAllowAll, prefixes) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Read only", http.StatusMethodNotAllowed)
			return
		}
		if file, ok := assets[req.URL.Path]; ok {
			data, e := dashboardFiles.ReadFile("dashboard/" + file)
			if e != nil {
				http.NotFound(w, req)
				return
			}
			contentType := "text/html; charset=utf-8"
			if strings.HasSuffix(file, ".js") {
				contentType = "text/javascript; charset=utf-8"
			}
			if strings.HasSuffix(file, ".css") {
				contentType = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			if req.Method != http.MethodHead {
				_, _ = w.Write(data)
			}
			return
		}
		if req.URL.Path != "/api/status" {
			http.NotFound(w, req)
			return
		}
		router, ready := cache.snapshot()
		runtime := state.snapshot()
		now := time.Now().UTC()
		running := runtime.PID > 0 && now.Sub(runtime.Updated) <= 20*time.Second
		// No private settings, API tokens or command output are returned here.
		emulator := map[string]any{"running": running, "version": packageVersion, "raynet": runtime.Raynet, "cidr": runtime.CIDR, "discovery": runtime.Engine, "mdns": running && runtime.MDNS, "http": running && runtime.HTTP, "relay": running && runtime.Relay}
		if now.Before(runtime.Axiom.Expires) {
			emulator["axiom"] = runtime.Axiom.IP
		}
		if now.Before(runtime.App.Expires) {
			emulator["app"] = runtime.App.IP
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if req.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "ready": ready, "stale": !ready || now.Sub(router.Observed) > 45*time.Second, "router": router, "emulator": emulator})
		}
	})
}

type publicIPEntry struct {
	value   string
	checked time.Time
}

// One fixed HTTPS endpoint is checked at most once a minute per active device.
// A failed check clears the value rather than retaining a stale public address.
func refreshPublicIPs(v *RouterView, cache map[string]publicIPEntry) {
	now := time.Now()
	for i := range v.Internet.Links {
		link := &v.Internet.Links[i]
		if link.State != "active" || !safeInterface(link.Device) {
			continue
		}
		key := link.Device + "/" + link.IP
		old, ok := cache[key]
		if !ok || now.Sub(old.checked) > time.Minute {
			b, e := runCommand(3500*time.Millisecond, nil, findProgram("curl"), "--silent", "--fail", "--connect-timeout", "2", "--max-time", "3", "--proto", "=https", "--interface", link.Device, "https://api.ipify.org")
			old = publicIPEntry{checked: now}
			if e == nil && usablePublicIPv4(string(b)) {
				old.value = strings.TrimSpace(string(b))
			}
			cache[key] = old
		}
		link.PublicIP = old.value
	}
	if len(cache) > 64 {
		for k, v := range cache {
			if now.Sub(v.checked) > 2*time.Minute {
				delete(cache, k)
			}
		}
	}
}
func startDashboard(ctx context.Context, p Paths, c Settings, r Resolved, state *stateStore) (*http.Server, context.CancelFunc, error) {
	listener, e := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(c.DashboardPort)))
	if e != nil {
		return nil, func() {}, e
	}
	dc, cancel := context.WithCancel(ctx)
	cache := &dashboardCache{}
	server := &http.Server{Handler: dashboardHandler(c, r, state, cache), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	go func() { defer listener.Close(); _ = server.Serve(listener) }()
	go func() {
		publicIPs := map[string]publicIPEntry{}
		for {
			if dc.Err() != nil {
				return
			}
			round, stop := context.WithTimeout(dc, 14*time.Second)
			view := collectRouter(round, p, r, state.snapshot())
			stop()
			if dc.Err() != nil {
				return
			}
			refreshPublicIPs(&view, publicIPs)
			if validateRouterView(view) == nil {
				cache.replace(view)
			}
			select {
			case <-dc.Done():
				return
			case <-time.After(15 * time.Second):
			}
		}
	}()
	return server, cancel, nil
}

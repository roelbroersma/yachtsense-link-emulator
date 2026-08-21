/*
 * YachtSense Link Emulator VuCI page for RutOS 7.
 *
 * The page uses only Vue plus the VuCI-provided Axios/message helpers. This
 * avoids a dependency on private Teltonika widget libraries that can move
 * between RutOS maintenance releases.
 */

const VueRuntime = globalThis.Vue;
const h = VueRuntime && VueRuntime.h;

// API responses are wrapped slightly differently by several RutOS 7 releases.
// Walk common wrapper keys until the expected status payload is found.
function findPayload(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 6) return null;
  if (value.config && value.status && Array.isArray(value.interfaces)) return value;

  for (const key of ["data", "result", "response"]) {
    const nested = findPayload(value[key], depth + 1);
    if (nested) return nested;
  }
  return null;
}

// Keep package-specific CSS local to this component.
const styles = `
.ysle-page{max-width:1180px;margin:0 auto;padding:4px}.ysle-header{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin-bottom:16px}.ysle-header h2{margin:0 0 4px;font-size:24px}.ysle-header p{margin:0;color:#667085}.ysle-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.ysle-card{background:var(--card-background,#fff);border:1px solid rgba(120,130,145,.24);border-radius:10px;padding:18px;box-shadow:0 1px 2px rgba(16,24,40,.05)}.ysle-card.full{grid-column:1/-1}.ysle-card h3{margin:0 0 14px;font-size:17px}.ysle-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:13px 16px}.ysle-field{display:flex;flex-direction:column;gap:5px}.ysle-field.full{grid-column:1/-1}.ysle-field label{font-size:13px;font-weight:600}.ysle-field small{color:#667085;line-height:1.35}.ysle-field input,.ysle-field select{min-height:38px;padding:7px 10px;border:1px solid #cbd5e1;border-radius:6px;background:transparent;color:inherit;box-sizing:border-box;width:100%}.ysle-switch{display:flex;align-items:flex-start;gap:10px;padding:8px 0}.ysle-switch input{margin-top:3px;width:17px;height:17px}.ysle-switch b{display:block}.ysle-switch span span{display:block;color:#667085;font-size:13px;margin-top:2px}.ysle-checkgrid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:7px 12px;margin-top:5px}.ysle-check{display:flex;gap:8px;align-items:flex-start;padding:8px 9px;border:1px solid rgba(120,130,145,.20);border-radius:6px}.ysle-check input{margin-top:2px}.ysle-check b{display:block;font-size:13px}.ysle-check small{display:block;color:#667085;margin-top:2px}.ysle-status{display:flex;flex-wrap:wrap;gap:8px}.ysle-badge{display:inline-flex;align-items:center;gap:6px;padding:5px 9px;border-radius:999px;background:#f2f4f7;font-size:12px;font-weight:600}.ysle-badge.ok{background:#ecfdf3;color:#067647}.ysle-badge.bad{background:#fef3f2;color:#b42318}.ysle-badge.neutral{background:#f2f4f7;color:#475467}.ysle-badge.warn{background:#fffaeb;color:#b54708}.ysle-dot{width:7px;height:7px;border-radius:50%;background:currentColor}.ysle-details{display:grid;grid-template-columns:175px 1fr;gap:7px 12px;margin-top:14px;font-size:13px}.ysle-details dt{color:#667085}.ysle-details dd{margin:0;word-break:break-word}.ysle-actions{display:flex;flex-wrap:wrap;gap:9px;margin-top:18px}.ysle-btn{min-height:38px;padding:8px 14px;border-radius:6px;border:1px solid #98a2b3;background:transparent;color:inherit;font-weight:600;cursor:pointer}.ysle-btn.primary{background:#175cd3;border-color:#175cd3;color:#fff}.ysle-btn.danger{border-color:#d92d20;color:#b42318}.ysle-btn:disabled{opacity:.5;cursor:not-allowed}.ysle-log,.ysle-listeners{margin:0;overflow:auto;padding:13px;border-radius:7px;background:#101828;color:#e4e7ec;font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:pre-wrap}.ysle-log{min-height:200px;max-height:420px}.ysle-listeners{min-height:42px;max-height:150px;margin-top:12px}.ysle-muted{color:#667085;font-size:13px}.ysle-note{padding:10px 12px;background:#fffaeb;border:1px solid #fedf89;border-radius:7px;color:#93370d;font-size:13px;margin-top:12px}.ysle-info{padding:10px 12px;background:#eff8ff;border:1px solid #b2ddff;border-radius:7px;color:#175cd3;font-size:13px;margin-top:12px}@media(max-width:760px){.ysle-grid,.ysle-form,.ysle-checkgrid{grid-template-columns:1fr}.ysle-card.full,.ysle-field.full{grid-column:auto}.ysle-header{display:block}.ysle-details{grid-template-columns:1fr}.ysle-details dt{font-weight:600}}
`;

export default {
  name: "YachtSenseLinkEmulator",

  // Defaults match the clean UCI config and provide a useful skeleton while
  // the first status request is loading.
  data() {
    return {
      loading: false,
      dirty: false,
      timer: null,
      interfaces: [],
      logs: [],
      status: {},
      mdnsEnvironment: {},
      form: {
        enabled: false,
        mdns_enabled: true,
        web_enabled: true,
        axiom_interface: "br-lan",
        remote_interfaces: ["br-lan"],
        relay_mode: "auto",
        manage_ip: true,
        ipaddr: "198.18.0.1",
        prefix: 21,
        remove_ip_on_stop: true,
        serial: "AF002A4",
        version: "V142.242.530",
        hostname: "yachtsense-main",
        instance: "yachtsense-main Settings",
        ttl: 120,
        health_port: 7777,
        log_level: "info",
        log_lines: 40,
      },
    };
  },

  mounted() {
    this.refresh(true);
    this.timer = setInterval(() => this.refresh(false), 5000);
  },

  beforeUnmount() {
    if (this.timer) clearInterval(this.timer);
  },

  methods: {
    notify(type, text) {
      if (this.$message && this.$message[type]) this.$message[type](text);
    },

    mark(key, value) {
      this.form[key] = value;
      this.dirty = true;
    },

    // Load current configuration, interfaces, runtime status, mDNS environment
    // and logs without overwriting unsaved form edits.
    async refresh(forceForm) {
      try {
        const response = await this.$axios.get("/api/yachtsense-link-emulator/status");
        const payload = findPayload(response);
        if (!payload) throw new Error("Unexpected API response");

        this.status = payload.status || {};
        this.mdnsEnvironment = payload.mdns_environment || {};
        this.interfaces = payload.interfaces || [];
        this.logs = payload.logs || [];
        if (forceForm || !this.dirty) {
          this.form = {
            ...this.form,
            ...(payload.config || {}),
            remote_interfaces: Array.isArray(payload.config?.remote_interfaces)
              ? [...payload.config.remote_interfaces]
              : [],
          };
        }
      } catch (error) {
        if (forceForm) this.notify("error", "YachtSense Link Emulator status could not be loaded");
      }
    },

    async action(name, data = {}) {
      this.loading = true;
      if (this.$spin) this.$spin();
      try {
        const response = await this.$axios.post(
          `/api/yachtsense-link-emulator/actions/${name}`,
          { data },
        );
        const candidates = [
          response,
          response?.data,
          response?.data?.data,
          response?.result,
          response?.result?.data,
        ];
        const result = candidates.find(
          (entry) => entry && typeof entry === "object" && ("message" in entry || "ok" in entry),
        );

        if (result?.ok === false) this.notify("warning", result.message || "Action was not performed");
        else this.notify("success", result?.message || "Action completed");

        this.dirty = false;
        await new Promise((resolve) => setTimeout(resolve, 700));
        await this.refresh(true);
      } catch (error) {
        this.notify("error", "Action failed");
      } finally {
        if (this.$spin) this.$spin(false);
        this.loading = false;
      }
    },

    save() {
      this.action("save", {
        ...this.form,
        remote_interfaces: [...(this.form.remote_interfaces || [])],
        prefix: Number(this.form.prefix),
        ttl: Number(this.form.ttl),
        health_port: Number(this.form.health_port),
        log_lines: Number(this.form.log_lines),
      });
    },

    field(label, key, type = "text", help = "", attrs = {}) {
      const input = h("input", {
        type,
        value: this.form[key],
        min: attrs.min,
        max: attrs.max,
        disabled: attrs.disabled,
        onInput: (event) => this.mark(key, event.target.value),
      });
      return h("div", { class: ["ysle-field", attrs.full ? "full" : ""] }, [
        h("label", label),
        input,
        help ? h("small", help) : null,
      ]);
    },

    selectField(label, key, options, help = "", attrs = {}) {
      return h("div", { class: ["ysle-field", attrs.full ? "full" : ""] }, [
        h("label", label),
        h(
          "select",
          {
            value: this.form[key],
            onChange: (event) => this.mark(key, event.target.value),
          },
          options.map(([value, text]) => h("option", { value }, text)),
        ),
        help ? h("small", help) : null,
      ]);
    },

    toggle(label, key, help) {
      return h("label", { class: "ysle-switch" }, [
        h("input", {
          type: "checkbox",
          checked: !!this.form[key],
          onChange: (event) => this.mark(key, event.target.checked),
        }),
        h("span", [h("b", label), h("span", help)]),
      ]);
    },

    // Add/remove one app-side interface from the UCI list.
    toggleRemoteInterface(name, enabled) {
      const next = new Set(this.form.remote_interfaces || []);
      if (enabled) next.add(name);
      else next.delete(name);
      this.mark("remote_interfaces", [...next]);
    },

    badge(label, state = "neutral") {
      return h("span", { class: ["ysle-badge", state] }, [
        h("span", { class: "ysle-dot" }),
        label,
      ]);
    },

    interfaceLabel(item) {
      const addresses = Array.isArray(item.addresses) ? item.addresses.join(", ") : "";
      return `${item.name}${addresses ? ` — ${addresses}` : ""}`;
    },
  },

  render() {
    if (!h) return null;

    const interfaceOptions = this.interfaces.map((item) =>
      h("option", { value: item.name }, this.interfaceLabel(item)),
    );
    if (!this.interfaces.some((item) => item.name === this.form.axiom_interface)) {
      interfaceOptions.unshift(
        h("option", { value: this.form.axiom_interface }, this.form.axiom_interface),
      );
    }

    const axiomInterfaceSelect = h("div", { class: "ysle-field full" }, [
      h("label", "Axiom / RayNet interface"),
      h(
        "select",
        {
          value: this.form.axiom_interface,
          onChange: (event) => this.mark("axiom_interface", event.target.value),
        },
        interfaceOptions,
      ),
      h("small", "Bridge, VLAN or Ethernet interface on which the Axiom/RayNet side is connected."),
    ]);

    const remoteChecks = this.interfaces.map((item) => {
      const checked = (this.form.remote_interfaces || []).includes(item.name);
      return h("label", { class: "ysle-check" }, [
        h("input", {
          type: "checkbox",
          checked,
          onChange: (event) => this.toggleRemoteInterface(item.name, event.target.checked),
        }),
        h("span", [
          h("b", item.name),
          h("small", Array.isArray(item.addresses) && item.addresses.length
            ? item.addresses.join(", ")
            : "No IPv4 address detected"),
        ]),
      ]);
    });

    const logsText = this.logs.length
      ? this.logs.join("\n")
      : "No YachtSense Link Emulator log entries yet.";
    const listenerText = Array.isArray(this.mdnsEnvironment.listeners) && this.mdnsEnvironment.listeners.length
      ? this.mdnsEnvironment.listeners.join("\n")
      : "No UDP/5353 listeners reported.";

    const lastAxiom = this.status.last_axiom_ip
      ? `${this.status.last_axiom_name || "Axiom/MFD"} — ${this.status.last_axiom_ip}${this.status.last_axiom_port ? `:${this.status.last_axiom_port}` : ""}`
      : "Not detected yet";
    const lastRemote = this.status.last_remote_ip
      ? `${this.status.last_remote_ip}${this.status.last_remote_iface ? ` via ${this.status.last_remote_iface}` : ""}`
      : "Not detected yet";

    const relayConfigured = this.form.relay_mode !== "disabled";
    const relayState = this.status.relay_active
      ? "ok"
      : (relayConfigured ? "neutral" : "neutral");

    return h("div", { class: "ysle-page" }, [
      h("style", styles),
      h("div", { class: "ysle-header" }, [
        h("div", [
          h("h2", "YachtSense Link Emulator"),
          h(
            "p",
            "YachtSense discovery for Axiom plus selective Raymarine app mDNS relay between routed networks.",
          ),
        ]),
        h(
          "button",
          { class: "ysle-btn", disabled: this.loading, onClick: () => this.refresh(true) },
          "Refresh",
        ),
      ]),

      h("div", { class: "ysle-grid" }, [
        h("section", { class: "ysle-card" }, [
          h("h3", "Service"),
          this.toggle(
            "Enable YachtSense Link Emulator",
            "enabled",
            "Enable procd autostart and run the configured components.",
          ),
          this.toggle(
            "Enable YachtSense mDNS advertisement",
            "mdns_enabled",
            "Publish yachtsense-main Settings._http._tcp with id=E70640 <serial>.",
          ),
          this.toggle(
            "Enable HTTP health service",
            "web_enabled",
            "Return HTTP 200 on the Axiom-side connection-monitor port.",
          ),
          this.toggle(
            "Manage the RayNet IPv4 address",
            "manage_ip",
            "Add the configured 198.18.x.x address to the Axiom interface at startup.",
          ),
          this.toggle(
            "Remove managed address on stop",
            "remove_ip_on_stop",
            "Remove only an address this package added itself.",
          ),
        ]),

        h("section", { class: "ysle-card" }, [
          h("h3", "Current status"),
          h("div", { class: "ysle-status" }, [
            this.badge(this.status.running ? "Service running" : "Service stopped", this.status.running ? "ok" : "bad"),
            this.badge(this.status.mdns_active ? "YachtSense mDNS active" : "YachtSense mDNS inactive", this.status.mdns_active ? "ok" : "neutral"),
            this.badge(this.status.web_active ? "HTTP health active" : "HTTP health inactive", this.status.web_active ? "ok" : "neutral"),
            this.badge(this.status.relay_active ? "Discovery relay active" : "Discovery relay inactive", relayState),
          ]),
          h("dl", { class: "ysle-details" }, [
            h("dt", "Discovery"), h("dd", this.status.discovery || "—"),
            h("dt", "RayNet address"), h("dd", this.status.address || "—"),
            h("dt", "Relay decision"), h("dd", this.status.relay_reason || "—"),
            h("dt", "Last Axiom/MFD"), h("dd", lastAxiom),
            h("dt", "Last Axiom service"), h("dd", this.status.last_axiom_service || "—"),
            h("dt", "Last app/client"), h("dd", lastRemote),
            h("dt", "Last client service"), h("dd", this.status.last_remote_service || "—"),
            h("dt", "Last activity"), h("dd", this.status.last_activity || "—"),
            h("dt", "Health URL"), h("dd", this.status.web_url || "—"),
            h("dt", "PID"), h("dd", this.status.pid || "—"),
            h("dt", "Package / RutOS"), h("dd", `${this.status.package_version || "—"} / ${this.status.rutos || "—"}`),
          ]),
          h(
            "div",
            { class: "ysle-note" },
            "The relay only handles discovery. RTSP video and touch/control remain normal routed unicast traffic to the Axiom.",
          ),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("h3", "Axiom / RayNet network"),
          h("div", { class: "ysle-form" }, [
            axiomInterfaceSelect,
            this.field(
              "RayNet IPv4 address",
              "ipaddr",
              "text",
              "Router address used on the Axiom side; default 198.18.0.1.",
            ),
            this.field("Prefix length", "prefix", "number", "Default: /21.", { min: 0, max: 32 }),
          ]),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("h3", "Raymarine app discovery relay"),
          h("div", { class: "ysle-form" }, [
            this.selectField(
              "Relay mode",
              "relay_mode",
              [
                ["auto", "Automatic (recommended)"],
                ["force", "Force built-in relay"],
                ["disabled", "Disabled"],
              ],
              "Automatic relays only between different interfaces and disables itself when an active Avahi reflector is detected.",
              { full: true },
            ),
            h("div", { class: "ysle-field full" }, [
              h("label", "Raymarine app interfaces"),
              h("div", { class: "ysle-checkgrid" }, remoteChecks),
              h("small", "Select one or more LAN/Wi-Fi/VLAN interfaces where phones or tablets run the Raymarine app."),
            ]),
            this.selectField(
              "Log level",
              "log_level",
              [
                ["info", "Info — device/client events"],
                ["debug", "Debug — include relay packets and follow-up queries"],
              ],
              "Info avoids multicast log spam; Debug is useful while commissioning.",
            ),
            this.field(
              "Log lines",
              "log_lines",
              "number",
              "Number of recent service log lines shown below.",
              { min: 5, max: 200 },
            ),
          ]),
          h(
            "div",
            { class: "ysle-info" },
            "Allowed discovery: _http._tcp, _rtsp._tcp, _rym_rrc._tcp, _raydb._tcp and _services._dns-sd._udp plus learned SRV/host follow-up names. Ordinary DNS on port 53 is not reflected.",
          ),
        ]),

        h("section", { class: "ysle-card" }, [
          h("h3", "mDNS / UDP 5353 environment"),
          h("div", { class: "ysle-status" }, [
            this.badge(
              this.mdnsEnvironment.port_in_use ? "UDP/5353 listener detected" : "UDP/5353 appears free",
              this.mdnsEnvironment.port_in_use ? "warn" : "ok",
            ),
            this.badge(
              this.mdnsEnvironment.avahi_running ? "Avahi running" : "Avahi not detected",
              this.mdnsEnvironment.avahi_running ? "neutral" : "ok",
            ),
            this.badge(
              this.mdnsEnvironment.avahi_reflector ? "Avahi reflector active" : "No Avahi reflector",
              this.mdnsEnvironment.avahi_reflector ? "warn" : "ok",
            ),
            this.badge(
              this.mdnsEnvironment.umdns_running ? "umdns running" : "umdns not detected",
              "neutral",
            ),
          ]),
          h("p", { class: "ysle-muted" }, this.mdnsEnvironment.message || "mDNS environment not checked yet."),
          h("pre", { class: "ysle-listeners" }, listenerText),
        ]),

        h("section", { class: "ysle-card" }, [
          h("h3", "YachtSense identity"),
          h("div", { class: "ysle-form" }, [
            this.field("Serial suffix", "serial", "text", "Published as id=E70640 <serial>."),
            this.field("Firmware version", "version", "text", "Published in the DNS-SD TXT record."),
            this.field("mDNS hostname", "hostname", "text", "Default: yachtsense-main."),
            this.field("Service instance", "instance", "text", "Default: yachtsense-main Settings."),
            this.field("mDNS TTL", "ttl", "number", "DNS record lifetime in seconds.", { min: 1, max: 86400 }),
            this.field("HTTP health port", "health_port", "number", "Axiom liveness monitor; default 7777.", { min: 1, max: 65535 }),
          ]),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("div", { class: "ysle-actions" }, [
            h(
              "button",
              { class: "ysle-btn primary", disabled: this.loading, onClick: this.save },
              "Save & apply",
            ),
            h(
              "button",
              { class: "ysle-btn", disabled: this.loading || !this.status.running, onClick: () => this.action("restart") },
              "Restart",
            ),
            h(
              "button",
              { class: "ysle-btn", disabled: this.loading || this.status.running, onClick: () => this.action("start") },
              "Start",
            ),
            h(
              "button",
              { class: "ysle-btn danger", disabled: this.loading || !this.status.running, onClick: () => this.action("stop") },
              "Stop",
            ),
          ]),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("h3", "Recent log entries"),
          h("pre", { class: "ysle-log" }, logsText),
          h(
            "p",
            { class: "ysle-muted" },
            "Info examples: YachtSense advertisement active, Raymarine app/client query detected, Axiom/MFD detected, Axiom remote-control service detected and reflector conflicts. Status refreshes every five seconds.",
          ),
        ]),
      ]),
    ]);
  },
};

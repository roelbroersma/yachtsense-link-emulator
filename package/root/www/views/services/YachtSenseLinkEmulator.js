/*
 * YachtSense Link Emulator VuCI page for RutOS 7.
 *
 * The page intentionally uses only the Vue runtime and the VuCI-provided Axios
 * and message helpers. This keeps the package independent from private widget
 * libraries that may change between RutOS maintenance releases.
 */

const VueRuntime = globalThis.Vue;
const h = VueRuntime && VueRuntime.h;

// API responses are wrapped slightly differently by several RutOS 7 releases.
// Walk the common wrapper keys until the expected status payload is found.
function findPayload(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 6) return null;
  if (value.config && value.status && Array.isArray(value.interfaces)) return value;

  for (const key of ["data", "result", "response"]) {
    const nested = findPayload(value[key], depth + 1);
    if (nested) return nested;
  }
  return null;
}

// Keep all page-specific CSS local to the component to avoid modifying VuCI.
const styles = `
.ysle-page{max-width:1120px;margin:0 auto;padding:4px}.ysle-header{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin-bottom:16px}.ysle-header h2{margin:0 0 4px;font-size:24px}.ysle-header p{margin:0;color:#667085}.ysle-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.ysle-card{background:var(--card-background,#fff);border:1px solid rgba(120,130,145,.24);border-radius:10px;padding:18px;box-shadow:0 1px 2px rgba(16,24,40,.05)}.ysle-card.full{grid-column:1/-1}.ysle-card h3{margin:0 0 14px;font-size:17px}.ysle-form{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:13px 16px}.ysle-field{display:flex;flex-direction:column;gap:5px}.ysle-field.full{grid-column:1/-1}.ysle-field label{font-size:13px;font-weight:600}.ysle-field small{color:#667085;line-height:1.35}.ysle-field input,.ysle-field select{min-height:38px;padding:7px 10px;border:1px solid #cbd5e1;border-radius:6px;background:transparent;color:inherit;box-sizing:border-box;width:100%}.ysle-switch{display:flex;align-items:flex-start;gap:10px;padding:8px 0}.ysle-switch input{margin-top:3px;width:17px;height:17px}.ysle-switch b{display:block}.ysle-switch span span{display:block;color:#667085;font-size:13px;margin-top:2px}.ysle-status{display:flex;flex-wrap:wrap;gap:8px}.ysle-badge{display:inline-flex;align-items:center;gap:6px;padding:5px 9px;border-radius:999px;background:#f2f4f7;font-size:12px;font-weight:600}.ysle-badge.ok{background:#ecfdf3;color:#067647}.ysle-badge.bad{background:#fef3f2;color:#b42318}.ysle-dot{width:7px;height:7px;border-radius:50%;background:currentColor}.ysle-details{display:grid;grid-template-columns:155px 1fr;gap:7px 12px;margin-top:14px;font-size:13px}.ysle-details dt{color:#667085}.ysle-details dd{margin:0;word-break:break-word}.ysle-actions{display:flex;flex-wrap:wrap;gap:9px;margin-top:18px}.ysle-btn{min-height:38px;padding:8px 14px;border-radius:6px;border:1px solid #98a2b3;background:transparent;color:inherit;font-weight:600;cursor:pointer}.ysle-btn.primary{background:#175cd3;border-color:#175cd3;color:#fff}.ysle-btn.danger{border-color:#d92d20;color:#b42318}.ysle-btn:disabled{opacity:.5;cursor:not-allowed}.ysle-log{margin:0;min-height:180px;max-height:360px;overflow:auto;padding:13px;border-radius:7px;background:#101828;color:#e4e7ec;font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:pre-wrap}.ysle-muted{color:#667085;font-size:13px}.ysle-note{padding:10px 12px;background:#fffaeb;border:1px solid #fedf89;border-radius:7px;color:#93370d;font-size:13px;margin-top:12px}@media(max-width:760px){.ysle-grid,.ysle-form{grid-template-columns:1fr}.ysle-card.full,.ysle-field.full{grid-column:auto}.ysle-header{display:block}.ysle-details{grid-template-columns:1fr}.ysle-details dt{font-weight:600}}
`;

export default {
  name: "YachtSenseLinkEmulator",

  // Form defaults match the package UCI file and provide a useful skeleton
  // while the first status request is still loading.
  data() {
    return {
      loading: false,
      dirty: false,
      timer: null,
      interfaces: [],
      logs: [],
      status: {},
      form: {
        enabled: false,
        mdns_enabled: true,
        web_enabled: true,
        interface: "br-lan",
        manage_ip: true,
        ipaddr: "198.18.0.1",
        prefix: 21,
        remove_ip_on_stop: true,
        serial: "RUTX001",
        version: "V142.242.530",
        hostname: "yachtsense-link",
        instance: "YachtSense Link Settings",
        ttl: 120,
        health_port: 7777,
        log_lines: 24,
      },
    };
  },

  // Refresh status periodically, but never overwrite unsaved user edits.
  mounted() {
    this.refresh(true);
    this.timer = setInterval(() => this.refresh(false), 5000);
  },

  beforeUnmount() {
    if (this.timer) clearInterval(this.timer);
  },

  methods: {
    // Use the notification helper supplied by VuCI when it is available.
    notify(type, text) {
      if (this.$message && this.$message[type]) this.$message[type](text);
    },

    // Mark one edited value so background refreshes preserve the form.
    mark(key, value) {
      this.form[key] = value;
      this.dirty = true;
    },

    // Load current configuration, interfaces, runtime status and log lines.
    async refresh(forceForm) {
      try {
        const response = await this.$axios.get("/api/yachtsense-link-emulator/status");
        const payload = findPayload(response);
        if (!payload) throw new Error("Unexpected API response");

        this.status = payload.status || {};
        this.interfaces = payload.interfaces || [];
        this.logs = payload.logs || [];
        if (forceForm || !this.dirty) {
          this.form = { ...this.form, ...(payload.config || {}) };
        }
      } catch (error) {
        if (forceForm) this.notify("error", "YachtSense Link Emulator status could not be loaded");
      }
    },

    // Execute a service API action and refresh after procd has applied it.
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
        await new Promise((resolve) => setTimeout(resolve, 650));
        await this.refresh(true);
      } catch (error) {
        this.notify("error", "Action failed");
      } finally {
        if (this.$spin) this.$spin(false);
        this.loading = false;
      }
    },

    // Send a complete, typed form so the API can atomically validate it.
    save() {
      this.action("save", {
        ...this.form,
        prefix: Number(this.form.prefix),
        ttl: Number(this.form.ttl),
        health_port: Number(this.form.health_port),
        log_lines: Number(this.form.log_lines),
      });
    },

    // Render a reusable text or numeric input field.
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

    // Render an independently controlled UCI boolean option.
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

    // Render one compact runtime-status indicator.
    badge(label, ok) {
      return h("span", { class: ["ysle-badge", ok ? "ok" : "bad"] }, [
        h("span", { class: "ysle-dot" }),
        label,
      ]);
    },
  },

  // The render function avoids a compile-time dependency on Vue templates.
  render() {
    if (!h) return null;

    const interfaceOptions = this.interfaces.map((item) => {
      const addresses = Array.isArray(item.addresses) ? item.addresses.join(", ") : "";
      return h(
        "option",
        { value: item.name },
        `${item.name}${addresses ? ` — ${addresses}` : ""}`,
      );
    });
    if (!this.interfaces.some((item) => item.name === this.form.interface)) {
      interfaceOptions.unshift(h("option", { value: this.form.interface }, this.form.interface));
    }

    const interfaceSelect = h("div", { class: "ysle-field full" }, [
      h("label", "Axiom / RayNet interface"),
      h(
        "select",
        {
          value: this.form.interface,
          onChange: (event) => this.mark("interface", event.target.value),
        },
        interfaceOptions,
      ),
      h("small", "Select the bridge or Ethernet interface connected to the Axiom network."),
    ]);

    const logsText = this.logs.length
      ? this.logs.join("\n")
      : "No YachtSense Link Emulator log entries yet.";

    return h("div", { class: "ysle-page" }, [
      h("style", styles),
      h("div", { class: "ysle-header" }, [
        h("div", [
          h("h2", "YachtSense Link Emulator"),
          h(
            "p",
            "Emulates the Raymarine YachtSense Link discovery identity and connection-monitor endpoint.",
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
            "Enable mDNS advertisement",
            "mdns_enabled",
            "Publish _http._tcp.local with id=E70640 <serial>.",
          ),
          this.toggle(
            "Enable HTTP health service",
            "web_enabled",
            "Return HTTP 200 on the configured connection-monitor port.",
          ),
          this.toggle(
            "Manage the IPv4 address",
            "manage_ip",
            "Add the configured address to the selected interface at startup.",
          ),
          this.toggle(
            "Remove managed address on stop",
            "remove_ip_on_stop",
            "Remove only an address that this package added itself.",
          ),
        ]),

        h("section", { class: "ysle-card" }, [
          h("h3", "Current status"),
          h("div", { class: "ysle-status" }, [
            this.badge(this.status.running ? "Service running" : "Service stopped", this.status.running),
            this.badge(this.status.mdns_active ? "mDNS active" : "mDNS inactive", this.status.mdns_active),
            this.badge(this.status.web_active ? "HTTP active" : "HTTP inactive", this.status.web_active),
            this.badge(
              this.status.interface_exists ? "Interface present" : "Interface missing",
              this.status.interface_exists,
            ),
            this.badge(
              this.status.address_present ? "Address present" : "Address missing",
              this.status.address_present,
            ),
          ]),
          h("dl", { class: "ysle-details" }, [
            h("dt", "Discovery"), h("dd", this.status.discovery || "—"),
            h("dt", "Address"), h("dd", this.status.address || "—"),
            h("dt", "Health URL"), h("dd", this.status.web_url || "—"),
            h("dt", "PID"), h("dd", this.status.pid || "—"),
            h("dt", "Package"), h("dd", this.status.package_version || "—"),
            h("dt", "RutOS"), h("dd", this.status.rutos || "—"),
            h("dt", "Architecture"), h("dd", this.status.architecture || "—"),
          ]),
          h(
            "div",
            { class: "ysle-note" },
            "Discovery emulation does not configure DHCP, DNS, routing or NAT. Those remain normal RutOS network settings.",
          ),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("h3", "Network and identity"),
          h("div", { class: "ysle-form" }, [
            interfaceSelect,
            this.field(
              "IPv4 address",
              "ipaddr",
              "text",
              "Default YachtSense/RayNet-side address used by the emulator.",
            ),
            this.field("Prefix length", "prefix", "number", "Default: 21.", { min: 0, max: 32 }),
            this.field(
              "Serial suffix",
              "serial",
              "text",
              "Advertised as id=E70640 <serial>.",
            ),
            this.field("Firmware version", "version", "text", "Published in the DNS-SD TXT record."),
            this.field("mDNS hostname", "hostname", "text", "Single label without .local."),
            this.field("Service instance", "instance", "text", "Human-readable DNS-SD instance name."),
            this.field("mDNS TTL", "ttl", "number", "DNS record lifetime in seconds.", {
              min: 1,
              max: 86400,
            }),
            this.field(
              "HTTP health port",
              "health_port",
              "number",
              "Axiom connection monitor; default: 7777.",
              { min: 1, max: 65535 },
            ),
            this.field(
              "Log lines",
              "log_lines",
              "number",
              "Number of recent entries shown below.",
              { min: 5, max: 100 },
            ),
          ]),
          h("div", { class: "ysle-actions" }, [
            h(
              "button",
              { class: "ysle-btn primary", disabled: this.loading, onClick: this.save },
              "Save & apply",
            ),
            h(
              "button",
              {
                class: "ysle-btn",
                disabled: this.loading || !this.status.running,
                onClick: () => this.action("restart"),
              },
              "Restart",
            ),
            h(
              "button",
              {
                class: "ysle-btn",
                disabled: this.loading || this.status.running,
                onClick: () => this.action("start"),
              },
              "Start",
            ),
            h(
              "button",
              {
                class: "ysle-btn danger",
                disabled: this.loading || !this.status.running,
                onClick: () => this.action("stop"),
              },
              "Stop",
            ),
          ]),
        ]),

        h("section", { class: "ysle-card full" }, [
          h("h3", "Recent log entries"),
          h("pre", { class: "ysle-log" }, logsText),
          h("p", { class: "ysle-muted" }, "Runtime status and logs refresh every five seconds."),
        ]),
      ]),
    ]);
  },
};

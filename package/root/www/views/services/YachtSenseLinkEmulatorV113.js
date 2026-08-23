/*
 * v1.0.13 status-first YachtSense Link Emulator UI.
 *
 * The normal path is intentionally simple: observe the existing network,
 * explain the automatic choice, and only expose overrides on demand.
 */

const VueRuntime = globalThis.Vue;
const h = VueRuntime && VueRuntime.h;
const STYLE_ID = "ysle-v113-stylesheet";
const STYLE_HREF = "/assets/yachtsense-link-emulator-v113.css?v=1.0.13";

function ensureStylesheet() {
  if (document.getElementById(STYLE_ID)) return;
  const link = document.createElement("link");
  link.id = STYLE_ID;
  link.rel = "stylesheet";
  link.href = STYLE_HREF;
  document.head.appendChild(link);
}

function findPayload(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 7) return null;
  if (value.config && value.status && Array.isArray(value.interfaces)) return value;
  for (const key of ["data", "result", "response"]) {
    const nested = findPayload(value[key], depth + 1);
    if (nested) return nested;
  }
  return null;
}

function findActionResult(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 7) return null;
  if ("ok" in value || "message" in value) return value;
  for (const key of ["data", "result", "response"]) {
    const nested = findActionResult(value[key], depth + 1);
    if (nested) return nested;
  }
  return null;
}

function errorMessage(error) {
  const data = error?.response?.data;
  const result = findActionResult(data);
  if (result?.message) return result.message;
  if (error?.code === "ECONNABORTED") return "Request timed out";
  return data?.error || error?.message || "Action failed";
}

function isPrivateIPv4(cidr) {
  const address = String(cidr || "").split("/")[0];
  if (/^10\./.test(address) || /^192\.168\./.test(address)) return true;
  const match = address.match(/^172\.(\d+)\./);
  return !!match && Number(match[1]) >= 16 && Number(match[1]) <= 31;
}

export default {
  name: "YachtSenseLinkEmulatorV113",

  data() {
    return {
      loading: true,
      saving: false,
      restarting: false,
      dirty: false,
      timer: null,
      interfaces: [],
      status: {},
      mdnsEnvironment: {},
      logs: [],
      editRaynet: false,
      editApp: false,
      editDiscovery: false,
      showDiscoveryHelp: false,
      raynetChoice: "auto",
      appChoice: "auto",
      form: {
        enabled: false,
        mdns_enabled: true,
        web_enabled: true,
        axiom_interface: "br-lan",
        remote_interfaces: ["br-lan"],
        relay_mode: "auto",
        manage_ip: false,
        ipaddr: "198.18.0.1",
        prefix: 21,
        remove_ip_on_stop: false,
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
    ensureStylesheet();
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

    raynetCidr() {
      return `${this.form.ipaddr}/${Number(this.form.prefix) || 21}`;
    },

    interfaceByName(name) {
      return (this.interfaces || []).find((item) => item.name === name) || null;
    },

    detectedRaynet() {
      const wanted = this.raynetCidr();
      return (this.interfaces || []).find(
        (item) => Array.isArray(item.addresses) && item.addresses.includes(wanted),
      ) || null;
    },

    detectedApp() {
      const raynet = this.raynetChoice === "manual"
        ? this.form.axiom_interface
        : (this.detectedRaynet()?.name || this.form.axiom_interface);
      const candidates = (this.interfaces || []).filter(
        (item) => item.name !== raynet && Array.isArray(item.addresses) && item.addresses.length,
      );
      const bridge = candidates.find((item) => item.name === "br-lan");
      if (bridge) return bridge;
      const configured = (this.form.remote_interfaces || [])
        .map((name) => candidates.find((item) => item.name === name))
        .find(Boolean);
      if (configured) return configured;
      const privateInterface = candidates.find((item) => item.addresses.some(isPrivateIPv4));
      return privateInterface || candidates[0] || null;
    },

    effectiveRaynet() {
      if (this.form.manage_ip || this.raynetChoice === "manual") {
        return this.interfaceByName(this.form.axiom_interface);
      }
      return this.detectedRaynet() || this.interfaceByName(this.form.axiom_interface);
    },

    effectiveAppInterfaces() {
      if (this.appChoice === "manual") return [...(this.form.remote_interfaces || [])];
      const detected = this.detectedApp();
      return detected ? [detected.name] : [...(this.form.remote_interfaces || [])];
    },

    sameDiscoveryNetwork() {
      const raynet = this.effectiveRaynet()?.name;
      return !!raynet && this.effectiveAppInterfaces().includes(raynet);
    },

    discoveryMode() {
      if (this.form.relay_mode === "force") return "builtin";
      if (this.form.relay_mode === "disabled") {
        return this.mdnsEnvironment.avahi_reflector ? "avahi" : "disabled";
      }
      return "auto";
    },

    discoveryModeLabel() {
      const mode = this.discoveryMode();
      if (mode === "builtin") return "Built-in daemon";
      if (mode === "avahi") return "Existing Avahi reflector";
      if (mode === "disabled") return "Relay disabled";
      return "Automatic";
    },

    discoveryEngine() {
      if (!this.status.running) return { label: "Not active", detail: "The service is not running.", state: "neutral" };
      if (this.sameDiscoveryNetwork()) {
        return { label: "Direct discovery", detail: "Both sides are on the same interface; no reflector is needed.", state: "ok" };
      }
      if (this.form.relay_mode === "force") {
        return { label: "Built-in daemon", detail: "The YachtSense daemon is handling discovery relay.", state: "ok" };
      }
      if (this.form.relay_mode === "disabled") {
        if (this.mdnsEnvironment.avahi_reflector) {
          return { label: "Existing Avahi reflector", detail: "The existing Avahi reflector handles cross-network mDNS.", state: "ok" };
        }
        return { label: "Relay disabled", detail: "No cross-network discovery relay is selected.", state: "bad" };
      }
      if (this.mdnsEnvironment.avahi_reflector) {
        return { label: "Existing Avahi reflector", detail: "Automatic mode detected Avahi and avoids running a second reflector.", state: "ok" };
      }
      return { label: "Built-in daemon", detail: "Automatic mode selected the built-in discovery relay.", state: "ok" };
    },

    overall() {
      if (!this.status.running) {
        if (this.form.enabled) {
          return { state: "bad", icon: "!", title: "YachtSense Link Emulator needs attention", text: "It is enabled in configuration, but the service is not currently running." };
        }
        return { state: "off", icon: "○", title: "YachtSense Link Emulator is off", text: "Turn it on when you want YachtSense discovery on this router." };
      }
      if (!this.detectedRaynet() && !this.form.manage_ip) {
        return { state: "bad", icon: "!", title: "RayNet address was not found", text: `${this.raynetCidr()} is not currently present on a router interface.` };
      }
      if (!this.status.last_axiom_ip) {
        return { state: "wait", icon: "…", title: "Waiting for an Axiom", text: "The service is ready; no Axiom/MFD discovery has been seen yet." };
      }
      if (!this.detectedApp()) {
        return { state: "wait", icon: "…", title: "Axiom found — waiting for an app network", text: "RayNet is working, but no suitable app-side IPv4 network is currently visible." };
      }
      const engine = this.discoveryEngine();
      if (engine.state === "bad") {
        return { state: "bad", icon: "!", title: "Axiom found, but discovery relay is unavailable", text: engine.detail };
      }
      return { state: "ok", icon: "✓", title: "YachtSense Link Emulator is working", text: `Axiom ${this.status.last_axiom_ip} is visible and discovery is handled by ${engine.label.toLowerCase()}.` };
    },

    async refresh(forceForm = false) {
      try {
        const response = await this.$axios.get(
          "/api/yachtsense-link-emulator-status-v110/status",
          { timeout: 5000 },
        );
        const payload = findPayload(response);
        if (!payload) throw new Error("Unexpected status response");
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
              : ["br-lan"],
          };
          if (!this.form.manage_ip && this.detectedRaynet()) this.raynetChoice = "auto";
          else this.raynetChoice = "manual";
          const app = this.detectedApp();
          this.appChoice = app && (this.form.remote_interfaces || []).length === 1
            && this.form.remote_interfaces[0] === app.name ? "auto" : "manual";
        }
      } catch (error) {
        if (forceForm) this.notify("error", errorMessage(error));
      } finally {
        this.loading = false;
      }
    },

    chooseRaynetAuto() {
      const detected = this.detectedRaynet();
      if (!detected) return;
      this.raynetChoice = "auto";
      this.form.axiom_interface = detected.name;
      this.form.manage_ip = false;
      this.form.remove_ip_on_stop = false;
      this.dirty = true;
    },

    chooseRaynetManual() {
      this.raynetChoice = "manual";
      this.dirty = true;
    },

    chooseAppAuto() {
      const detected = this.detectedApp();
      if (!detected) return;
      this.appChoice = "auto";
      this.form.remote_interfaces = [detected.name];
      this.dirty = true;
    },

    chooseAppManual() {
      this.appChoice = "manual";
      this.dirty = true;
    },

    toggleRemote(name, enabled) {
      const next = new Set(this.form.remote_interfaces || []);
      if (enabled) next.add(name); else next.delete(name);
      this.form.remote_interfaces = [...next];
      this.appChoice = "manual";
      this.dirty = true;
    },

    setDiscoveryMode(mode) {
      if (mode === "auto") this.mark("relay_mode", "auto");
      else if (mode === "builtin") this.mark("relay_mode", "force");
      else if (mode === "avahi" && this.mdnsEnvironment.avahi_reflector) this.mark("relay_mode", "disabled");
      this.editDiscovery = false;
    },

    async save() {
      if (this.saving) return;
      const raynet = this.raynetChoice === "auto" ? this.detectedRaynet() : this.interfaceByName(this.form.axiom_interface);
      if (!this.form.manage_ip && !raynet) {
        this.notify("error", `RayNet ${this.raynetCidr()} could not be detected on any interface.`);
        return;
      }
      const remotes = this.effectiveAppInterfaces();
      if (!remotes.length) {
        this.notify("error", "No app-side IPv4 network could be detected. Choose one under Networks.");
        return;
      }

      this.saving = true;
      try {
        const data = {
          ...this.form,
          axiom_interface: raynet?.name || this.form.axiom_interface,
          remote_interfaces: remotes,
          prefix: Number(this.form.prefix),
          ttl: Number(this.form.ttl),
          health_port: Number(this.form.health_port),
          log_lines: Number(this.form.log_lines),
        };
        const response = await this.$axios.post(
          "/api/yachtsense-link-emulator-save-v112/actions/save",
          { data: { payload: JSON.stringify(data) } },
          { timeout: 10000 },
        );
        const result = findActionResult(response);
        if (result?.ok === false) {
          this.notify("error", result.message || "Configuration could not be saved");
          return;
        }
        this.form = { ...this.form, axiom_interface: data.axiom_interface, remote_interfaces: [...data.remote_interfaces] };
        this.dirty = false;
        this.notify("success", "Changes saved");
        setTimeout(() => this.refresh(true), 1400);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        this.saving = false;
      }
    },

    async discard() {
      this.dirty = false;
      this.editRaynet = false;
      this.editApp = false;
      this.editDiscovery = false;
      await this.refresh(true);
    },

    async restart() {
      if (this.restarting) return;
      this.restarting = true;
      try {
        const response = await this.$axios.post(
          "/api/yachtsense-link-emulator-control-v112/actions/restart",
          { data: {} },
          { timeout: 10000 },
        );
        const result = findActionResult(response);
        if (result?.ok === false) this.notify("error", result.message || "Restart could not be scheduled");
        else this.notify("success", "Restart scheduled");
        setTimeout(() => this.refresh(true), 1400);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        this.restarting = false;
      }
    },

    statusRow(label, main, detail, state, stateText) {
      return h("div", { class: "ysle13-status-row" }, [
        h("div", { class: "ysle13-row-label" }, label),
        h("div", { class: "ysle13-row-main" }, [main, detail ? h("span", { class: "ysle13-row-sub" }, detail) : null]),
        h("span", { class: ["ysle13-state", state] }, stateText),
      ]);
    },

    settingRow(label, mainChildren, actionText, action) {
      return h("div", { class: "ysle13-setting-row" }, [
        h("div", { class: "ysle13-row-label" }, label),
        h("div", { class: "ysle13-row-main" }, mainChildren),
        h("button", { class: "ysle13-btn", type: "button", onClick: action }, actionText),
      ]);
    },

    field(label, key, help, attrs = {}) {
      return h("div", { class: ["ysle13-field", attrs.full ? "full" : ""] }, [
        h("label", label),
        h("input", {
          class: "ysle13-input",
          type: attrs.type || "text",
          value: this.form[key],
          min: attrs.min,
          max: attrs.max,
          disabled: attrs.disabled,
          onInput: (event) => this.mark(key, event.target.value),
        }),
        help ? h("small", help) : null,
      ]);
    },
  },

  render() {
    if (!h) return null;

    const overall = this.overall();
    const raynet = this.detectedRaynet();
    const effectiveRaynet = this.effectiveRaynet();
    const app = this.detectedApp();
    const engine = this.discoveryEngine();
    const lastAxiom = this.status.last_axiom_ip
      ? `${this.status.last_axiom_name || "Axiom/MFD"} — ${this.status.last_axiom_ip}${this.status.last_axiom_port ? `:${this.status.last_axiom_port}` : ""}`
      : "Not detected yet";

    const statusRows = [
      this.statusRow(
        "Service",
        this.status.running ? `Running${this.status.pid ? ` · PID ${this.status.pid}` : ""}` : "Not running",
        this.status.autostart ? "Starts automatically with the router" : "Autostart is currently off",
        this.status.running ? "ok" : (this.form.enabled ? "bad" : "neutral"),
        this.status.running ? "Active" : (this.form.enabled ? "Attention" : "Off"),
      ),
      this.statusRow(
        "RayNet",
        raynet ? `${raynet.name} · ${this.raynetCidr()}` : this.raynetCidr(),
        raynet ? "Detected from the address already present on the router" : (this.form.manage_ip ? "Address management is enabled under Advanced" : "No matching interface detected"),
        raynet || this.form.manage_ip ? "ok" : "bad",
        raynet ? "Detected" : (this.form.manage_ip ? "Managed" : "Missing"),
      ),
      this.statusRow(
        "Axiom",
        lastAxiom,
        this.status.last_axiom_service || "Waiting for Raymarine discovery traffic",
        this.status.last_axiom_ip ? "ok" : "wait",
        this.status.last_axiom_ip ? "Detected" : "Waiting",
      ),
      this.statusRow(
        "App network",
        app ? `${app.name} · ${(app.addresses || []).join(", ")}` : "No suitable IPv4 network detected",
        app ? "Recommended automatically for Raymarine apps" : "Choose a network under Networks",
        app ? "ok" : "wait",
        app ? "Ready" : "Waiting",
      ),
      this.statusRow(
        "Discovery",
        engine.label,
        engine.detail,
        engine.state,
        engine.state === "ok" ? "Working" : (engine.state === "bad" ? "Attention" : "Idle"),
      ),
    ];

    const raynetOptions = (this.interfaces || []).map((item) =>
      h("option", { value: item.name }, `${item.name}${item.addresses?.length ? ` — ${item.addresses.join(", ")}` : ""}`),
    );

    const appChecks = (this.interfaces || [])
      .filter((item) => item.name !== effectiveRaynet?.name && Array.isArray(item.addresses) && item.addresses.length)
      .map((item) => h("label", { class: "ysle13-check" }, [
        h("input", {
          type: "checkbox",
          checked: (this.form.remote_interfaces || []).includes(item.name),
          onChange: (event) => this.toggleRemote(item.name, event.target.checked),
        }),
        h("span", [h("b", item.name), h("small", item.addresses.join(", "))]),
      ]));

    const raynetEditor = this.editRaynet ? h("div", { class: "ysle13-editor" }, [
      h("label", { class: ["ysle13-radio", raynet ? "" : "disabled"] }, [
        h("input", {
          type: "radio",
          name: "raynet-mode",
          checked: this.raynetChoice === "auto",
          disabled: !raynet,
          onChange: () => this.chooseRaynetAuto(),
        }),
        h("span", [
          h("b", "Automatic (recommended)"),
          h("small", raynet ? `Use ${raynet.name}, detected because ${this.raynetCidr()} is already present there.` : `${this.raynetCidr()} is not currently present on any interface.`),
        ]),
      ]),
      h("label", { class: "ysle13-radio" }, [
        h("input", {
          type: "radio",
          name: "raynet-mode",
          checked: this.raynetChoice === "manual",
          onChange: () => this.chooseRaynetManual(),
        }),
        h("span", [h("b", "Choose interface manually"), h("small", "Only needed for unusual network layouts or when the emulator manages the RayNet address.")]),
      ]),
      this.raynetChoice === "manual" ? h("select", {
        class: "ysle13-select",
        value: this.form.axiom_interface,
        onChange: (event) => this.mark("axiom_interface", event.target.value),
      }, raynetOptions) : null,
    ]) : null;

    const appEditor = this.editApp ? h("div", { class: "ysle13-editor" }, [
      h("label", { class: ["ysle13-radio", app ? "" : "disabled"] }, [
        h("input", {
          type: "radio",
          name: "app-mode",
          checked: this.appChoice === "auto",
          disabled: !app,
          onChange: () => this.chooseAppAuto(),
        }),
        h("span", [
          h("b", "Automatic (recommended)"),
          h("small", app ? `Use ${app.name} (${app.addresses.join(", ")}).` : "No app-side IPv4 network is currently detectable."),
        ]),
      ]),
      h("label", { class: "ysle13-radio" }, [
        h("input", {
          type: "radio",
          name: "app-mode",
          checked: this.appChoice === "manual",
          onChange: () => this.chooseAppManual(),
        }),
        h("span", [h("b", "Choose app network(s) manually"), h("small", "Useful when phones or tablets can arrive from more than one routed network.")]),
      ]),
      this.appChoice === "manual" ? h("div", appChecks.length ? appChecks : h("small", "No other IPv4 interfaces are currently available.")) : null,
    ]) : null;

    const discoveryEditor = this.editDiscovery ? h("div", { class: "ysle13-editor" }, [
      h("label", { class: "ysle13-radio" }, [
        h("input", { type: "radio", name: "discovery-mode", checked: this.discoveryMode() === "auto", onChange: () => this.setDiscoveryMode("auto") }),
        h("span", [h("b", "Automatic (recommended)"), h("small", "Use an existing Avahi reflector when present; otherwise use the built-in daemon. No relay is used when both networks are the same.")]),
      ]),
      h("label", { class: "ysle13-radio" }, [
        h("input", { type: "radio", name: "discovery-mode", checked: this.discoveryMode() === "builtin", onChange: () => this.setDiscoveryMode("builtin") }),
        h("span", [h("b", "Built-in daemon"), h("small", "Force the YachtSense Link Emulator to handle cross-network Raymarine discovery itself.")]),
      ]),
      h("label", { class: ["ysle13-radio", this.mdnsEnvironment.avahi_reflector ? "" : "disabled"] }, [
        h("input", {
          type: "radio",
          name: "discovery-mode",
          checked: this.discoveryMode() === "avahi",
          disabled: !this.mdnsEnvironment.avahi_reflector,
          onChange: () => this.setDiscoveryMode("avahi"),
        }),
        h("span", [
          h("b", "Existing Avahi reflector"),
          h("small", this.mdnsEnvironment.avahi_reflector ? "An Avahi reflector is detected and can handle mDNS between the networks." : "Not available because no Avahi reflector is currently detected."),
        ]),
      ]),
    ]) : null;

    const listenerText = Array.isArray(this.mdnsEnvironment.listeners) && this.mdnsEnvironment.listeners.length
      ? this.mdnsEnvironment.listeners.join("\n")
      : "No UDP/5353 listeners reported.";
    const logsText = this.logs.length ? this.logs.join("\n") : "No YachtSense Link Emulator log entries yet.";

    return h("div", { class: "ysle13-page" }, [
      h("header", { class: "ysle13-header" }, [
        h("div", { class: "ysle13-title" }, [
          h("h2", "YachtSense Link Emulator"),
          h("p", `Package ${this.status.package_version || "…"} · Raymarine discovery without changing your network by default`),
        ]),
        h("label", { class: "ysle13-master" }, [
          h("span", this.form.enabled ? "On" : "Off"),
          h("input", {
            type: "checkbox",
            checked: !!this.form.enabled,
            disabled: this.loading || this.saving,
            onChange: (event) => this.mark("enabled", event.target.checked),
          }),
        ]),
      ]),

      h("section", { class: ["ysle13-card", "ysle13-hero"] }, [
        h("div", { class: "ysle13-hero-head" }, [
          h("div", { class: ["ysle13-hero-icon", overall.state] }, overall.icon),
          h("div", { class: "ysle13-hero-copy" }, [h("h3", overall.title), h("p", overall.text)]),
        ]),
        h("div", { class: "ysle13-status-list" }, statusRows),
      ]),

      h("section", { class: "ysle13-card" }, [
        h("div", { class: "ysle13-section-title" }, [
          h("h3", "Networks"),
          h("p", "The emulator observes addresses that already exist. It only changes an IP address when you explicitly enable that under Advanced."),
        ]),
        h("div", { class: "ysle13-settings" }, [
          this.settingRow(
            "RayNet",
            [
              raynet ? `${raynet.name} · ${this.raynetCidr()}` : (this.form.manage_ip ? `${this.form.axiom_interface} · ${this.raynetCidr()}` : `Not detected · ${this.raynetCidr()}`),
              h("span", { class: ["ysle13-pill", raynet ? "ok" : ""] }, this.form.manage_ip ? "Managed" : "Automatic"),
              h("span", { class: "ysle13-row-sub" }, raynet ? "Detected from the IP already configured on the router" : "No network setting is changed automatically"),
            ],
            this.editRaynet ? "Done" : "Change…",
            () => { this.editRaynet = !this.editRaynet; },
          ),
          this.settingRow(
            "Raymarine app network",
            [
              app ? `${app.name} · ${app.addresses.join(", ")}` : "Not detected",
              h("span", { class: "ysle13-pill" }, this.appChoice === "auto" ? "Automatic" : "Manual"),
              h("span", { class: "ysle13-row-sub" }, "Where phones/tablets running the Raymarine app connect"),
            ],
            this.editApp ? "Done" : "Change…",
            () => { this.editApp = !this.editApp; },
          ),
        ]),
        raynetEditor,
        appEditor,
      ]),

      h("section", { class: "ysle13-card" }, [
        h("div", { class: "ysle13-section-title" }, [
          h("h3", "Discovery"),
          h("p", "One job, one visible choice. Automatic normally makes the right decision."),
        ]),
        h("div", { class: "ysle13-settings" }, [
          this.settingRow(
            "Mode",
            [
              this.discoveryModeLabel(),
              h("span", { class: "ysle13-pill" }, this.discoveryMode() === "auto" ? "Recommended" : "Override"),
              h("span", { class: "ysle13-row-sub" }, `Currently using: ${engine.label}`),
            ],
            this.editDiscovery ? "Done" : "Change…",
            () => { this.editDiscovery = !this.editDiscovery; },
          ),
        ]),
        discoveryEditor,
        h("div", { class: "ysle13-settings" }, [
          h("button", { class: "ysle13-btn", type: "button", onClick: () => { this.showDiscoveryHelp = !this.showDiscoveryHelp; } }, this.showDiscoveryHelp ? "Hide explanation" : "ⓘ How discovery is handled"),
        ]),
        this.showDiscoveryHelp ? h("div", { class: "ysle13-info" }, [
          h("b", "Automatic means the router chooses the least invasive path. "),
          "If both sides share one interface, nothing is reflected. If an Avahi reflector already exists, it is reused. Otherwise the YachtSense built-in daemon relays the Raymarine discovery traffic. A normal Avahi responder is shown in Diagnostics but is not mistaken for a reflector.",
        ]) : null,
      ]),

      h("details", { class: "ysle13-details" }, [
        h("summary", "Advanced"),
        h("div", { class: "ysle13-details-body" }, [
          h("p", { class: "ysle13-details-intro" }, "These settings are normally best left alone. Network address management is off by default so the emulator observes your existing RayNet setup instead of changing it."),
          h("label", { class: "ysle13-toggle" }, [
            h("input", { type: "checkbox", checked: !!this.form.manage_ip, onChange: (event) => { this.mark("manage_ip", event.target.checked); if (!event.target.checked) this.form.remove_ip_on_stop = false; } }),
            h("span", [h("b", "Manage RayNet IPv4 address"), h("small", this.form.manage_ip ? `The emulator may add ${this.raynetCidr()} to the selected RayNet interface.` : `Off — ${this.raynetCidr()} must already exist and is only detected.`)]),
          ]),
          h("label", { class: "ysle13-toggle" }, [
            h("input", { type: "checkbox", checked: !!this.form.remove_ip_on_stop, disabled: !this.form.manage_ip, onChange: (event) => this.mark("remove_ip_on_stop", event.target.checked) }),
            h("span", [h("b", "Remove managed IP when stopped"), h("small", "Only applies to an address that this package added itself.")]),
          ]),
          h("label", { class: "ysle13-toggle" }, [
            h("input", { type: "checkbox", checked: !!this.form.mdns_enabled, onChange: (event) => this.mark("mdns_enabled", event.target.checked) }),
            h("span", [h("b", "YachtSense mDNS advertisement"), h("small", "Normally required. Publishes the YachtSense Link identity to the Axiom.")]),
          ]),
          h("label", { class: "ysle13-toggle" }, [
            h("input", { type: "checkbox", checked: !!this.form.web_enabled, onChange: (event) => this.mark("web_enabled", event.target.checked) }),
            h("span", [h("b", "HTTP health service"), h("small", "Normally required. Provides the YachtSense connection monitor on the configured port.")]),
          ]),
          h("div", { class: "ysle13-form-grid" }, [
            this.field("RayNet IPv4", "ipaddr", "Address expected on the RayNet side."),
            this.field("Prefix", "prefix", "CIDR prefix length.", { type: "number", min: 0, max: 32 }),
            this.field("Serial suffix", "serial", "Published as part of the YachtSense identity."),
            this.field("Reported firmware", "version", "Identity string advertised to the Axiom."),
            this.field("mDNS hostname", "hostname", "Default: yachtsense-main"),
            this.field("Service instance", "instance", "Default: yachtsense-main Settings"),
            this.field("Health port", "health_port", "Default: 7777", { type: "number", min: 1, max: 65535 }),
            this.field("mDNS TTL", "ttl", "Default: 120 seconds", { type: "number", min: 1, max: 86400 }),
            h("div", { class: "ysle13-field" }, [
              h("label", "Log level"),
              h("select", { class: "ysle13-select", value: this.form.log_level, onChange: (event) => this.mark("log_level", event.target.value) }, [
                h("option", { value: "info" }, "Info"),
                h("option", { value: "debug" }, "Debug"),
              ]),
              h("small", "Debug is useful temporarily when diagnosing discovery traffic."),
            ]),
            this.field("Log lines", "log_lines", "Number retained in the diagnostics view.", { type: "number", min: 5, max: 200 }),
          ]),
        ]),
      ]),

      h("details", { class: "ysle13-details" }, [
        h("summary", "Diagnostics"),
        h("div", { class: "ysle13-details-body" }, [
          h("p", { class: "ysle13-details-intro" }, "Technical details for troubleshooting. These do not need attention while the main status says the emulator is working."),
          h("dl", { class: "ysle13-tech" }, [
            h("dt", "Package"), h("dd", this.status.package_version || "Unknown"),
            h("dt", "RutOS"), h("dd", this.status.rutos || "Unknown"),
            h("dt", "Process"), h("dd", this.status.running ? `PID ${this.status.pid || "?"}` : "Not running"),
            h("dt", "Configured RayNet"), h("dd", `${this.form.axiom_interface} · ${this.raynetCidr()}`),
            h("dt", "Detected RayNet"), h("dd", raynet ? `${raynet.name} · ${this.raynetCidr()}` : "Not detected"),
            h("dt", "Last Axiom"), h("dd", lastAxiom),
            h("dt", "Last app/client"), h("dd", this.status.last_remote_ip ? `${this.status.last_remote_ip}${this.status.last_remote_iface ? ` via ${this.status.last_remote_iface}` : ""}` : "Not detected yet"),
            h("dt", "Discovery engine"), h("dd", `${engine.label} — ${engine.detail}`),
            h("dt", "Avahi"), h("dd", this.mdnsEnvironment.avahi_running ? (this.mdnsEnvironment.avahi_reflector ? "Running · reflector active" : "Running · responder only") : "Not running"),
            h("dt", "umdns"), h("dd", this.mdnsEnvironment.umdns_running ? "Running" : "Not running"),
            h("dt", "UCI config"), h("dd", this.status.uci_config_dir || "Unknown"),
            h("dt", "UDP/5353"), h("dd", listenerText),
          ]),
          h("div", { class: "ysle13-actions" }, [
            h("button", { class: ["ysle13-btn", "secondary"], type: "button", disabled: this.loading, onClick: () => this.refresh(true) }, "Refresh status"),
            h("button", { class: ["ysle13-btn", "secondary"], type: "button", disabled: this.restarting || !this.status.running, onClick: () => this.restart() }, this.restarting ? "Restarting…" : "Restart service"),
          ]),
          h("details", { class: "ysle13-details", style: "margin-top:14px;margin-bottom:0" }, [
            h("summary", "Logs"),
            h("div", { class: "ysle13-details-body" }, [h("pre", { class: "ysle13-log" }, logsText)]),
          ]),
        ]),
      ]),

      this.dirty ? h("div", { class: "ysle13-savebar" }, [
        h("span", "You have unsaved changes"),
        h("div", { class: "ysle13-savebar-actions" }, [
          h("button", { class: "ysle13-btn", type: "button", disabled: this.saving, onClick: () => this.discard() }, "Cancel"),
          h("button", { class: ["ysle13-btn", "primary"], type: "button", disabled: this.saving, onClick: () => this.save() }, this.saving ? "Saving…" : "Save changes"),
        ]),
      ]) : null,
    ]);
  },
};

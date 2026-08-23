/*
 * v1.0.12 entry point.
 * Backend service transitions are asynchronous. VuCI requests have explicit
 * timeouts and use local button state instead of the full-page global spinner.
 */

import YachtSenseLinkEmulator from "./YachtSenseLinkEmulatorV110.js?v=1.0.12";

function findPayload(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 7) return null;
  if (value.config && value.status && Array.isArray(value.interfaces)) return value;
  for (const key of ["data", "result", "response"]) {
    const result = findPayload(value[key], depth + 1);
    if (result) return result;
  }
  return null;
}

function findActionResult(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 7) return null;
  if ("ok" in value || "message" in value) return value;
  for (const key of ["data", "result", "response"]) {
    const result = findActionResult(value[key], depth + 1);
    if (result) return result;
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

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorV112",

  methods: {
    ...(YachtSenseLinkEmulator.methods || {}),

    async refresh(forceForm) {
      if (this.refreshInFlight) return;
      this.refreshInFlight = true;
      try {
        const response = await this.$axios.get(
          "/api/yachtsense-link-emulator-status-v110/status",
          { timeout: 5000 },
        );
        const payload = findPayload(response);
        if (!payload) throw new Error("Unexpected status response");

        this.status = payload.status || {};
        this.mdnsEnvironment = payload.mdns_environment || {};
        this.logs = payload.logs || [];

        if (payload.interfaces_snapshot_ok !== false || !(this.interfaces || []).length) {
          this.interfaces = (payload.interfaces || []).map((item) => {
            const addresses = Array.isArray(item.addresses) ? item.addresses : [];
            if (addresses.length) {
              this.stableInterfaceAddresses[item.name] = [...addresses];
              this.interfaceAddressMisses[item.name] = 0;
              return item;
            }
            const misses = (this.interfaceAddressMisses[item.name] || 0) + 1;
            this.interfaceAddressMisses[item.name] = misses;
            const stable = this.stableInterfaceAddresses[item.name];
            if (Array.isArray(stable) && stable.length && misses <= 6) {
              return { ...item, addresses: [...stable] };
            }
            return item;
          });
        }

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
        if (forceForm) this.notify("error", errorMessage(error));
      } finally {
        this.refreshInFlight = false;
      }
    },

    async save() {
      this.loading = true;
      try {
        const data = {
          ...this.form,
          remote_interfaces: [...(this.form.remote_interfaces || [])],
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
        this.notify("success", result?.message || "Configuration saved");
        this.dirty = false;
        setTimeout(() => this.refresh(true), 1200);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        this.loading = false;
      }
    },

    async action(name, data = {}) {
      if (!["start", "restart", "stop"].includes(name)) return undefined;
      this.loading = true;
      try {
        const response = await this.$axios.post(
          `/api/yachtsense-link-emulator-control-v112/actions/${name}`,
          { data },
          { timeout: 10000 },
        );
        const result = findActionResult(response);
        if (result?.ok === false) this.notify("error", result.message || `Service ${name} failed`);
        else this.notify("success", result?.message || `Service ${name} scheduled`);
        setTimeout(() => this.refresh(true), 1200);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        this.loading = false;
      }
    },
  },
};

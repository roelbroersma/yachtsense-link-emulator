/*
 * v1.0.9 entry point.
 *
 * Uses a single-snapshot status API, native-CLI UCI save/control endpoints and a
 * small CSS override for the native checkbox focus artifact.
 */

import YachtSenseLinkEmulator from "./YachtSenseLinkEmulatorStyled.js?v=1.0.9";

const originalMounted = YachtSenseLinkEmulator.mounted;

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
  return result?.message || data?.error || error?.message || "Action failed";
}

function ensureV109Stylesheet() {
  const id = "ysle-v109-stylesheet";
  if (document.getElementById(id)) return;
  const link = document.createElement("link");
  link.id = id;
  link.rel = "stylesheet";
  link.href = "/assets/yachtsense-link-emulator-v109.css";
  document.head.appendChild(link);
}

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorV109",

  methods: {
    ...(YachtSenseLinkEmulator.methods || {}),

    async refresh(forceForm) {
      if (this.refreshInFlight) return;
      this.refreshInFlight = true;
      try {
        const response = await this.$axios.get("/api/yachtsense-link-emulator-status-v109/status");
        const payload = findPayload(response);
        if (!payload) throw new Error("Unexpected status response");

        this.status = payload.status || {};
        this.mdnsEnvironment = payload.mdns_environment || {};
        this.logs = payload.logs || [];

        // Keep the previous complete interface list when the single kernel
        // address snapshot itself failed. For individual transient empty values,
        // retain the most recent CIDR for up to six polls (about 30 seconds).
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
        if (forceForm) this.notify("error", errorMessage(error) || "Status could not be loaded");
      } finally {
        this.refreshInFlight = false;
      }
    },

    async save() {
      this.loading = true;
      if (this.$spin) this.$spin();
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
          "/api/yachtsense-link-emulator-save-v109/actions/save",
          { data: { payload: JSON.stringify(data) } },
        );
        const result = findActionResult(response);
        if (result?.ok === false) {
          this.notify("error", result.message || "Configuration could not be saved");
          return;
        }
        this.notify("success", result?.message || "Configuration saved and applied");
        this.dirty = false;
        await new Promise((resolve) => setTimeout(resolve, 500));
        await this.refresh(true);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        if (this.$spin) this.$spin(false);
        this.loading = false;
      }
    },

    async action(name, data = {}) {
      if (!["start", "restart", "stop"].includes(name)) return undefined;

      this.loading = true;
      if (this.$spin) this.$spin();
      try {
        const response = await this.$axios.post(
          `/api/yachtsense-link-emulator-control-v109/actions/${name}`,
          { data },
        );
        const result = findActionResult(response);
        if (result?.ok === false) this.notify("error", result.message || `Service ${name} failed`);
        else this.notify("success", result?.message || `Service ${name} completed`);
        await new Promise((resolve) => setTimeout(resolve, 500));
        await this.refresh(true);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        if (this.$spin) this.$spin(false);
        this.loading = false;
      }
    },
  },

  mounted() {
    ensureV109Stylesheet();
    if (typeof originalMounted === "function") originalMounted.call(this);
  },
};

/*
 * CSP-safe wrapper for the YachtSense Link Emulator VuCI page.
 * RutOS allows stylesheets from 'self' but blocks arbitrary inline <style> tags.
 *
 * This wrapper also serializes status refreshes and briefly keeps the last good
 * interface address snapshot so transient/incomplete API samples do not make
 * addresses visibly jump between a CIDR and "No IPv4 address detected".
 */

import YachtSenseLinkEmulator from "./YachtSenseLinkEmulator.js";

const STYLE_ID = "ysle-stylesheet";
const STYLE_HREF = "/assets/yachtsense-link-emulator.css";

function ensureStylesheet() {
  if (document.getElementById(STYLE_ID)) return;

  const link = document.createElement("link");
  link.id = STYLE_ID;
  link.rel = "stylesheet";
  link.href = STYLE_HREF;
  document.head.appendChild(link);
}

function findActionResult(value, depth = 0) {
  if (!value || typeof value !== "object" || depth > 6) return null;
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

const originalData = YachtSenseLinkEmulator.data;
const originalMounted = YachtSenseLinkEmulator.mounted;
const originalRefresh = YachtSenseLinkEmulator.methods.refresh;

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorStyled",

  data() {
    return {
      ...originalData.call(this),
      refreshInFlight: false,
      stableInterfaceAddresses: {},
      interfaceAddressMisses: {},
    };
  },

  methods: {
    ...YachtSenseLinkEmulator.methods,

    // Never allow the five-second timer to overlap a previous status request.
    // Two consecutive empty samples are treated as transient; a third empty
    // sample is accepted so a genuinely removed address still disappears.
    async refresh(forceForm) {
      if (this.refreshInFlight) return;
      this.refreshInFlight = true;
      try {
        await originalRefresh.call(this, forceForm);

        this.interfaces = (this.interfaces || []).map((item) => {
          const addresses = Array.isArray(item.addresses) ? item.addresses : [];
          if (addresses.length) {
            this.stableInterfaceAddresses[item.name] = [...addresses];
            this.interfaceAddressMisses[item.name] = 0;
            return item;
          }

          const misses = (this.interfaceAddressMisses[item.name] || 0) + 1;
          this.interfaceAddressMisses[item.name] = misses;
          const stable = this.stableInterfaceAddresses[item.name];
          if (Array.isArray(stable) && stable.length && misses <= 2) {
            return { ...item, addresses: [...stable] };
          }
          return item;
        });
      } finally {
        this.refreshInFlight = false;
      }
    },

    // Use a dedicated save endpoint that writes UCI list values with add_list.
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
          "/api/yachtsense-link-emulator-config/actions/save",
          { data },
        );
        const result = findActionResult(response);
        if (result?.ok === false) {
          this.notify("error", result.message || "Configuration could not be saved");
          return;
        }

        this.notify("success", result?.message || "Configuration saved and applied");
        this.dirty = false;
        await new Promise((resolve) => setTimeout(resolve, 700));
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
    ensureStylesheet();
    if (typeof originalMounted === "function") originalMounted.call(this);
  },
};

/*
 * v1.0.5 entry point.
 *
 * Save transports the form as one JSON string to avoid RutOS FunctionService
 * boolean/list schema quirks. Service controls verify that the daemon remains
 * alive before reporting success.
 */

import YachtSenseLinkEmulator from "./YachtSenseLinkEmulatorV104.js?v=1.0.5";

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

const originalAction = YachtSenseLinkEmulator.methods?.action;

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorV105",

  methods: {
    ...(YachtSenseLinkEmulator.methods || {}),

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
          "/api/yachtsense-link-emulator-save-v105/actions/save",
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
      if (!["start", "restart", "stop"].includes(name)) {
        return originalAction ? originalAction.call(this, name, data) : undefined;
      }

      this.loading = true;
      if (this.$spin) this.$spin();
      try {
        const response = await this.$axios.post(
          `/api/yachtsense-link-emulator-control-v105/actions/${name}`,
          { data: {} },
        );
        const result = findActionResult(response);
        if (result?.ok === false) {
          this.notify("error", result.message || `Service ${name} failed`);
        } else {
          this.notify("success", result?.message || `Service ${name} completed`);
        }
        await new Promise((resolve) => setTimeout(resolve, 400));
        await this.refresh(true);
      } catch (error) {
        this.notify("error", errorMessage(error));
      } finally {
        if (this.$spin) this.$spin(false);
        this.loading = false;
      }
    },
  },
};

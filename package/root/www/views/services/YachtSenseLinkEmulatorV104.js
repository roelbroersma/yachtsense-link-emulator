/*
 * v1.0.4 entry point. The versioned module name and import query deliberately
 * bust RutOS/browser module caching after package upgrades.
 */

import YachtSenseLinkEmulator from "./YachtSenseLinkEmulatorStyled.js?v=1.0.4";

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

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorV104",

  methods: {
    ...(YachtSenseLinkEmulator.methods || {}),

    // Use the schema-safe save API whose remote_interfaces option is explicitly
    // declared as a RutOS FunctionService list.
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
          "/api/yachtsense-link-emulator-save/actions/save",
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
};

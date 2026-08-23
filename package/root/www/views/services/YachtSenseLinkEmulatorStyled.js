/*
 * CSP-safe wrapper for the YachtSense Link Emulator VuCI page.
 * RutOS allows stylesheets from 'self' but blocks arbitrary inline <style> tags.
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

const originalMounted = YachtSenseLinkEmulator.mounted;

export default {
  ...YachtSenseLinkEmulator,
  name: "YachtSenseLinkEmulatorStyled",

  mounted() {
    ensureStylesheet();
    if (typeof originalMounted === "function") originalMounted.call(this);
  },
};

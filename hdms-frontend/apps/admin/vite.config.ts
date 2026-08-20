import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
const srcDir = fileURLToPath(new URL("./src", import.meta.url));

// HTTPS in dev, not just production, because the admin app shares an
// origin story with the kiosk (docs/phases/phase-0-foundations.md, 0.2) —
// and because a self-signed-looking browser warning during development
// hides the day it starts meaning something.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5174,
    https: {
      cert: readFileSync(`${certsDir}/localhost.pem`),
      key: readFileSync(`${certsDir}/localhost-key.pem`),
    },
    proxy: {
      "/v1": {
        target: process.env.VITE_API_TARGET || "https://localhost:8443",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});

import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
const srcDir = fileURLToPath(new URL("./src", import.meta.url));
const certFile = `${certsDir}/localhost.pem`;
const keyFile = `${certsDir}/localhost-key.pem`;
const hasCerts = existsSync(certFile) && existsSync(keyFile);

// The recovery page talks only to the worker (/recovery/api/*), never to the
// API. In dev the request goes through the dev Caddy on :8443, which forwards
// it to worker:8090 — the worker port is not published. HTTPS matters here:
// the session cookie is Secure, and over plain HTTP the browser drops it.
export default defineConfig({
  // Production serves the page under /recovery behind Caddy.
  base: process.env.VITE_BASE_PATH ?? "/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5176,
    https: hasCerts ? { cert: readFileSync(certFile), key: readFileSync(keyFile) } : undefined,
    proxy: {
      "/recovery/api": {
        target: process.env.VITE_RECOVERY_TARGET || "https://localhost:8443",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});

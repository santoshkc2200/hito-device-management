import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
const srcDir = fileURLToPath(new URL("./src", import.meta.url));

// The dev server needs HTTPS (see below), but a production `vite build`
// must not require the mkcert material — the Docker builder (5.3a) has no
// certs and only needs the static output.
const certFile = `${certsDir}/localhost.pem`;
const keyFile = `${certsDir}/localhost-key.pem`;
const hasCerts = existsSync(certFile) && existsSync(keyFile);

// HTTPS in dev, not just production, because the admin app shares an
// origin story with the kiosk (docs/phases/phase-0-foundations.md, 0.2) —
// and because a self-signed-looking browser warning during development
// hides the day it starts meaning something.
export default defineConfig({
  // Production serves the admin console under /admin behind Caddy
  // (docs/09-security-privacy-ops.md, 5.3a). The base is override-only so
  // `pnpm dev` and existing tests keep running at /.
  base: process.env.VITE_BASE_PATH ?? "/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5174,
    https: hasCerts
      ? {
          cert: readFileSync(certFile),
          key: readFileSync(keyFile),
        }
      : undefined,
    proxy: {
      "/v1": {
        target: process.env.VITE_API_TARGET || "https://localhost:8443",
        changeOrigin: true,
        secure: false,
      },
    },
  },
});

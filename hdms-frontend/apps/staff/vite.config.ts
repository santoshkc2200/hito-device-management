import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

let certsDir = "";
let srcDir = "";

try {
  const metaUrl = new URL(import.meta.url);
  if (metaUrl.protocol === "file:") {
    certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
    srcDir = fileURLToPath(new URL("./src", import.meta.url));
  }
} catch {
  // Fallback for non-file URL environments
}

if (!certsDir) {
  const currentDir = (import.meta as any).dirname ?? process.cwd();
  certsDir = path.resolve(currentDir, "../../../certs");
}
if (!srcDir) {
  const currentDir = (import.meta as any).dirname ?? process.cwd();
  srcDir = path.resolve(currentDir, "src");
}

const certFile = `${certsDir}/localhost.pem`;
const keyFile = `${certsDir}/localhost-key.pem`;
const hasCerts = existsSync(certFile) && existsSync(keyFile);

export const pwaOptions = {
  registerType: "autoUpdate" as const,
  workbox: {
    globPatterns: ["**/*.{js,css,html,ico,png,svg}"],
    navigateFallback: "index.html",
    // The staff app has no offline story in Phase 7: every data screen says
    // it needs a connection rather than serving a stale answer about who
    // has what.
    runtimeCaching: [{ urlPattern: /\/v1\/.*/, handler: "NetworkOnly" as const }],
  },
  manifest: {
    name: "HDMS Staff",
    short_name: "HDMS",
    description: "Hito Device Management System — your card, your loans, the device catalogue",
    display: "standalone" as const,
    orientation: "portrait" as const,
    start_url: "/",
    background_color: "#ffffff",
    theme_color: "#0a0a0a",
    icons: [
      { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
      { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
      { src: "apple-touch-icon.png", sizes: "180x180", type: "image/png", purpose: "maskable" },
    ],
  },
};

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA(pwaOptions),
  ],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5175,
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

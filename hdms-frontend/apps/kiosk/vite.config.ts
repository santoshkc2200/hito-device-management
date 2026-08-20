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
    globPatterns: ["**/*.{js,css,html,ico,png,svg,wasm}"],
    navigateFallback: "index.html",
    runtimeCaching: [
      {
        urlPattern: /\/v1\/.*/,
        handler: "NetworkOnly" as const,
      },
    ],
  },
  manifest: {
    name: "HDMS Kiosk",
    short_name: "HDMS Kiosk",
    description: "Hito Device Management System — borrow and return kiosk",
    display: "standalone" as const,
    orientation: "any" as const,
    start_url: "/",
    background_color: "#0a0a0a",
    theme_color: "#0a0a0a",
    icons: [
      { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
      { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
    ],
  },
};

// HTTPS in dev is not optional here: the camera fallback (BarcodeDetector)
// refuses to run over plain HTTP, and that's exactly the path this needs
// to exercise locally (docs/phases/phase-0-foundations.md, 0.2).
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
    port: 5173,
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

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

const certsDir = fileURLToPath(new URL("../../../certs", import.meta.url));
const srcDir = fileURLToPath(new URL("./src", import.meta.url));

// HTTPS in dev is not optional here: the camera fallback (BarcodeDetector)
// refuses to run over plain HTTP, and that's exactly the path this needs
// to exercise locally (docs/phases/phase-0-foundations.md, 0.2).
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      manifest: {
        name: "HDMS Kiosk",
        short_name: "HDMS Kiosk",
        description: "Hito Device Management System — borrow and return kiosk",
        display: "standalone",
        orientation: "any",
        start_url: "/",
        background_color: "#0a0a0a",
        theme_color: "#0a0a0a",
        icons: [
          { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
          { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      "@": srcDir,
    },
  },
  server: {
    port: 5173,
    https: {
      cert: readFileSync(`${certsDir}/localhost.pem`),
      key: readFileSync(`${certsDir}/localhost-key.pem`),
    },
  },
});

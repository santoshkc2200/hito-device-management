import { defineConfig, devices } from "@playwright/test";

// One smoke test per app (docs/phases/phase-0-foundations.md, 0.7). Each
// project starts its own Vite dev server; `ignoreHTTPSErrors` is needed in
// CI, where the mkcert root CA (see `task certs`) isn't installed in the
// browser's trust store even though the certificate itself is valid.
import { getFakeCameraLaunchArgs } from "./e2e/helpers/fake-camera";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: "list",
  use: {
    trace: "on-first-retry",
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "kiosk",
      testMatch: /kiosk\.spec\.ts/,
      use: {
        ...devices["Desktop Chrome"],
        baseURL: "https://localhost:5173",
        permissions: ["camera"],
        launchOptions: {
          args: getFakeCameraLaunchArgs(),
        },
      },
    },
    {
      name: "admin",
      testMatch: /admin\.spec\.ts/,
      use: { ...devices["Desktop Chrome"], baseURL: "https://localhost:5174" },
    },
    {
      name: "staff",
      testMatch: /staff-login\.spec\.ts/,
      use: { ...devices["Desktop Chrome"], baseURL: "https://localhost:5175" },
    },
  ],
  webServer: [
    {
      command: "pnpm --filter kiosk dev",
      url: "https://localhost:5173",
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
    },
    {
      command: "pnpm --filter admin dev",
      url: "https://localhost:5174",
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
    },
    {
      command: "pnpm --filter staff dev",
      url: "https://localhost:5175",
      reuseExistingServer: !process.env.CI,
      ignoreHTTPSErrors: true,
    },
  ],
});

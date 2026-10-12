import { defineConfig, devices } from "@playwright/test";

// Browser E2E (D-68). The dev server proxies the public API to
// localhost:8080 (D-69), so a running dev rig makes these tests exercise
// the real backend. Browsers are provisioned separately (CI installs
// them; locally: `npx playwright install chromium`).
export default defineConfig({
  testDir: "e2e",
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: "http://localhost:5173",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "npm run dev",
    url: "http://localhost:5173",
    reuseExistingServer: true,
    timeout: 60_000,
  },
});

import { defineConfig, devices } from "@playwright/test";

// The console's browser tiers (D-68), mirroring the backend's:
//  - e2e:         the UI in a real browser; the API is mocked per spec
//                 (e2e/*.spec.ts). Needs nothing running.
//  - integration: the console against the real API on the Tilt rig
//                 (e2e/*.rig.spec.ts): sessions, cookies, errors, email.
//  - uat:         the HLD-18 user journeys end to end on the rig, the way a
//                 person does them (e2e/uat/*.uat.spec.ts).
// Unit and component tests are Vitest (`npm test`). The dev server
// proxies the public API to localhost:8080 (D-69). Browsers are
// provisioned separately (locally: `npx playwright install chromium`).
const chrome = { ...devices["Desktop Chrome"] };

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
  projects: [
    { name: "e2e", testIgnore: [/\.rig\.spec\.ts$/, /\.uat\.spec\.ts$/], use: chrome },
    { name: "integration", testMatch: /\.rig\.spec\.ts$/, use: chrome },
    { name: "uat", testMatch: /\.uat\.spec\.ts$/, use: chrome },
  ],
  webServer: {
    command: "npm run dev",
    url: "http://localhost:5173",
    reuseExistingServer: true,
    timeout: 60_000,
  },
});

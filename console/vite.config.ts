/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import floors from "../quality/coverage-floors.json" with { type: "json" };

// The API is reached by relative URLs on the console's own origin (D-69).
// In development the dev server forwards the public ConnectRPC paths to the
// local API, so the HttpOnly refresh cookie behaves exactly as it does
// behind the production reverse proxy.
const apiTarget = "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/atsapbx.v1.": { target: apiTarget, changeOrigin: false },
    },
  },
  preview: {
    port: 4173,
    strictPort: true,
    proxy: {
      "/atsapbx.v1.": { target: apiTarget, changeOrigin: false },
    },
  },
  build: {
    sourcemap: false,
    target: "es2023",
  },
  test: {
    environment: "jsdom",
    environmentOptions: { jsdom: { url: "http://localhost:3000/" } },
    setupFiles: ["src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
    // The contrast gate reads the real token file (src/styles/
    // contrast.test.ts); Vitest otherwise empties CSS imports.
    css: { include: [/tokens\.css/] },
    // Coverage (quality-and-supply-chain-gates, D-71). Thresholds are the
    // floors in quality/coverage-floors.json: measured once, then only
    // ever raised. Generated clients and test helpers are not ours to
    // cover; the entry point is proven by the browser tiers.
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/api/gen/**", "src/test/**", "src/**/*.test.{ts,tsx}", "src/main.tsx", "src/vite-env.d.ts"],
      reporter: ["text-summary", "json-summary", "lcov", "cobertura"],
      reportsDirectory: "coverage",
      thresholds: floors.console,
    },
  },
});

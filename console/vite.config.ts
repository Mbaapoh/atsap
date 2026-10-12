/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

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
  },
});

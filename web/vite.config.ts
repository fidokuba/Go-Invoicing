/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Milestone 12: development proxies /api/v1 (and the unversioned health
// probes) straight to the Go API so the browser only ever talks to one
// origin (the Vite dev server) — no CORS configuration is needed in
// development, and none is added to the production Go server either. See
// the README's frontend development section for the full workflow.
//
// build.outDir points directly at internal/webui/dist — the directory
// internal/webui/webui.go embeds via go:embed — so `npm run build` alone
// produces exactly what the Go binary ships, with no separate copy step.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/health": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: true,
    // Vitest's own default include pattern would otherwise also pick up
    // web/e2e/*.spec.ts — a Playwright suite, run by `npm run e2e`
    // (playwright.config.ts), never by Vitest. Its `test` import
    // conflicts with Vitest's own global of the same name.
    exclude: ["**/node_modules/**", "e2e/**"],
  },
});

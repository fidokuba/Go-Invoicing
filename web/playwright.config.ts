import { defineConfig, devices } from "@playwright/test";

/**
 * End-to-end configuration (Milestone 12 section 39).
 *
 * This deliberately does NOT start its own `webServer` (unlike a typical
 * Playwright starter config): the target under test is the real Go API
 * plus a real PostgreSQL database, not something npm can spin up on its
 * own. See e2e/README.md for exactly how to bring that stack up before
 * running `npm run e2e` — CI does this itself as a dedicated job.
 */
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false, // the workflow spec is a single, ordered story against one shared backend
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8080",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});

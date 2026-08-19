import { defineConfig, devices } from "@playwright/test";

// Ports distinct from `make dev`'s 8080/4000 so `make test-e2e` can run
// alongside an already-running dev stack instead of colliding with it.
const API_PORT = 8081;
const WEB_PORT = 4100;
const API_URL = `http://localhost:${API_PORT}`;
const WEB_URL = `http://localhost:${WEB_PORT}`;

// The API needs a real, migrated Postgres — see docs/testing.md's E2E
// section. DATABASE_URL must already point at one (same precondition as
// `make test-integration`).
const databaseURL =
  process.env.DATABASE_URL ??
  "postgres://flowlens:flowlens@localhost:55432/flowlens?sslmode=disable";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? "html" : "list",

  use: {
    baseURL: WEB_URL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],

  // Starts a real API + web server for the run; both are torn down after.
  // reuseExistingServer lets a developer instead point these ports at
  // servers they started by hand.
  webServer: [
    {
      command: "go run ./cmd/api",
      cwd: "../api",
      url: `${API_URL}/healthz`,
      reuseExistingServer: !process.env.CI,
      env: {
        APP_ENV: "development",
        API_PORT: String(API_PORT),
        WEB_BASE_URL: WEB_URL,
        DATABASE_URL: databaseURL,
        SESSION_TTL_HOURS: process.env.SESSION_TTL_HOURS ?? "168",
        ENCRYPTION_KEY: process.env.ENCRYPTION_KEY ?? "",
        SYNC_WORKER_ENABLED: "false",
        // The suite's precondition is a database that is already migrated
        // (docs/testing.md), so leave the startup migration to the deploy
        // path it exists for rather than racing the harness here.
        RUN_MIGRATIONS: "false",
        // The web server below proxies the browser through to this API.
        TRUSTED_PROXY_HOPS: "1",
      },
    },
    {
      // Not `npm run dev -- -p ...`: that script already hardcodes `-p 4000`,
      // and appending a second `-p` is fragile. Invoke next directly instead.
      command: `npx next dev -p ${WEB_PORT}`,
      url: WEB_URL,
      reuseExistingServer: !process.env.CI,
      env: {
        API_INTERNAL_URL: API_URL,
        // NEXT_PUBLIC_API_BASE_URL is deliberately unset, so the browser
        // calls this origin and next.config.ts's rewrites proxy through to
        // the API. That is the shipped self-hosted topology, so it is the
        // one the e2e suite should exercise.
      },
    },
  ],
});

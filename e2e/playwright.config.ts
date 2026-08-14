import os from 'node:os'
import path from 'node:path'
import { defineConfig, devices } from '@playwright/test'

// Fixed, non-default ports so a run here doesn't collide with a feedla
// instance a developer might already have running locally.
const APP_PORT = 18099
const FIXTURE_PORT = 18098

// Unique per test-run DB so repeated runs start from a clean slate.
const dbPath = path.join(os.tmpdir(), `feedla-e2e-${process.pid}.db`)

export default defineConfig({
  testDir: './tests',
  fullyParallel: false, // tests share one feedla server/DB in this MVP
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${APP_PORT}`,
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: `node fixtures/feed-server.mjs ${FIXTURE_PORT}`,
      port: FIXTURE_PORT,
      reuseExistingServer: false,
    },
    {
      // Not `feedla serve`: this runs a build with the crawler's
      // SSRF-blocking dialer swapped out, since fixtures/feed-server.mjs is
      // a loopback address the production dialer correctly refuses to
      // fetch. See e2e/testserver/main.go's doc comment.
      command: 'go run ./e2e/testserver',
      cwd: '..',
      port: APP_PORT,
      reuseExistingServer: false,
      timeout: 30_000,
      env: {
        FR_DB_PATH: dbPath,
        FR_LISTEN: `127.0.0.1:${APP_PORT}`,
      },
    },
  ],
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})

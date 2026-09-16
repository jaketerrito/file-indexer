import process from 'node:process'
import { defineConfig, devices } from '@playwright/test'

// The e2e suite runs against the Tilt-deployed app in this checkout's
// namespace (never a local dev server): `just test-e2e` sets E2E_BASE_URL to
// http://web.<namespace>.localhost (the shared gateway) and relies on the
// crawler having indexed the seed dataset (deploy/overlays/local/seed/).
const baseURL = process.env.E2E_BASE_URL
if (!baseURL) {
  throw new Error(
    'E2E_BASE_URL is not set. Run via `just test-e2e` (requires `just up`), or set it to ' +
      'http://web.<namespace>.localhost for a running Tilt environment.',
  )
}

export default defineConfig({
  testDir: './e2e',
  // Indexing is asynchronous (crawler -> index workers -> search service),
  // so data-dependent assertions poll through reloads (see e2e/helpers.ts);
  // the per-test timeout must comfortably outlast a cold index of the seed set.
  timeout: 180_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: {
      args: [
        // *.localhost resolves to ::1 first under systemd-resolved while the
        // kind gateway proxy listens on 127.0.0.1 only; pinning the mapping
        // sidesteps resolver/fallback flakiness (see root AGENTS.md).
        '--host-resolver-rules=MAP *.localhost 127.0.0.1',
      ],
    },
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})

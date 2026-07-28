import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// Some sandboxed dev environments export HTTP_PROXY/HTTPS_PROXY/ALL_PROXY
// (and lowercase variants) pointing at a proxy that cannot reach localhost,
// which breaks both this config's own webServer readiness checks and the
// browser's navigation requests (both surface as an unexplained 502). A
// normal developer machine has none of these set, so clearing them here is a
// no-op outside such environments — and safe, since this suite only ever
// talks to localhost.
for (const key of ['HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy']) {
  delete process.env[key]
}

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(__dirname, '..')

const DSN = process.env.E2E_DATABASE_URL ??
  'postgres://bounty:bounty@localhost:5433/bountyboard?sslmode=disable'
const BACKEND_URL = process.env.E2E_BACKEND_URL ?? 'http://localhost:8080'
const WEB_URL = process.env.E2E_WEB_URL ?? 'http://localhost:5173'
const backendPort = new URL(BACKEND_URL).port || '8080'
const webPort = new URL(WEB_URL).port || '5173'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [['list']],
  timeout: 30_000,
  use: {
    baseURL: WEB_URL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // The sandboxed dev environment sets HTTP_PROXY/ALL_PROXY env vars that
    // point at a proxy which does not know how to reach localhost, returning
    // 502 for every request. Browsers on some platforms pick up these env
    // vars for proxy resolution; --no-proxy-server forces direct connections
    // regardless, which is also simply correct for a suite that only ever
    // talks to localhost.
    launchOptions: {
      args: ['--no-proxy-server'],
    },
  },
  projects: [
    {
      name: 'chromium',
      // The touch-target sweep belongs to the project below and only there:
      // run under this one it would silently measure a `pointer: fine`
      // context and prove nothing.
      testIgnore: /22-touch-targets\.spec\.ts/,
      use: { ...devices['Desktop Chrome'] },
    },
    {
      // A genuine coarse-pointer context. `hasTouch` is what makes Chromium
      // report `(pointer: coarse)` / `(hover: none)` — the media query
      // index.css's 44px floor is keyed on. Without a project like this
      // there is no way to exercise that rule at all from Playwright, and a
      // phone-width run under Desktop Chrome would pass whether the floor
      // exists or not.
      name: 'chromium-touch',
      testMatch: /22-touch-targets\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], hasTouch: true },
    },
  ],
  webServer: [
    {
      command: `sh -c "docker compose -f '${path.join(repoRoot, 'docker-compose.yml')}' up -d --wait && sleep infinity"`,
      port: 5433,
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: 'go run ./cmd/server',
      cwd: repoRoot,
      env: {
        DATABASE_URL: DSN,
        APP_ENV: 'development',
        AUTH_PROVIDER: 'development',
        APP_BASE_URL: WEB_URL,
        ADDR: `:${backendPort}`,
        SESSION_SECRET: 'cGxheXdyaWdodC1zZXNzaW9uLXNlY3JldC0zMi1ieXQ=',
      },
      url: `${BACKEND_URL}/api/users`,
      reuseExistingServer: true,
      timeout: 30_000,
    },
    {
      command: 'npm run dev',
      cwd: __dirname,
      env: {
        VITE_AUTH_PROVIDER: 'development',
        VITE_API_TARGET: BACKEND_URL,
        VITE_PORT: webPort,
      },
      url: WEB_URL,
      reuseExistingServer: true,
      timeout: 30_000,
    },
  ],
})

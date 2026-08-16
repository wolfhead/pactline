import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  outputDir: '../.fleet/playwright-results',
  reporter: 'line',
  use: {
    baseURL: 'http://127.0.0.1:7333',
    browserName: 'chromium',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run preview -- --host 127.0.0.1 --port 7333',
    url: 'http://127.0.0.1:7333',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})

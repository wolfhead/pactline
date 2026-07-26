/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8080' },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // The Playwright e2e suite lives under ./e2e and matches the same
    // *.spec.ts glob vitest would otherwise pick up by default. Keep the two
    // suites separate: `npm test` must run only the component tests, never
    // launch a browser.
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
  },
})

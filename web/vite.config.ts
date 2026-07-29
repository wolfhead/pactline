/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: Number(process.env.VITE_PORT ?? 5173),
    // Match the API namespace with its slash boundary. `/api-docs` is a
    // client-side route and must reach Vite's history fallback, not the Go
    // API proxy.
    proxy: { '^/api/': process.env.VITE_API_TARGET ?? 'http://localhost:8080' },
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

import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: { host: '127.0.0.1', port: 7332, proxy: { '/api': 'http://127.0.0.1:7331', '/livez': 'http://127.0.0.1:7331', '/readyz': 'http://127.0.0.1:7331' } },
  test: { environment: 'jsdom', setupFiles: ['./src/test-setup.ts'], include: ['src/**/*.test.{ts,tsx}'], css: true },
})

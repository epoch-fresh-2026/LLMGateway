import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    include: ['tests/**/*.test.{ts,tsx}'],
    setupFiles: ['tests/setup.ts'],
    testTimeout: 15000,
    coverage: {
      provider: 'istanbul',
      include: ['src/**/*.ts', 'src/**/*.tsx'],
      exclude: ['src/**/*.d.ts', 'src/types/api.ts', 'src/api/generated/schema.ts'],
      reporter: ['text', 'json', 'json-summary', 'html'],
      thresholds: { statements: 100 },
    },
  },
})

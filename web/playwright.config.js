import { defineConfig, devices } from '@playwright/test'
import os from 'node:os'
import path from 'node:path'

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  // Specs own isolated browser contexts and mock API state. Bound parallelism
  // instead of scaling to every host core (including on release runners).
  workers: 2,
  timeout: 20_000,
  expect: { timeout: 5_000 },
  reporter: 'line',
  outputDir: path.join(os.tmpdir(), 'xkeen-control-playwright'),
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: false,
    timeout: 30_000,
  },
})

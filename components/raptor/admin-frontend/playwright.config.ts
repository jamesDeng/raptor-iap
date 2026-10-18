import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests/browser",
  use: {
    baseURL: "http://127.0.0.1:5183",
    headless: true,
    channel: process.env.PLAYWRIGHT_CHANNEL,
    screenshot: "only-on-failure",
  },
  webServer: {
    command:
      process.env.RAPTOR_ADMIN_TEST_BINARY ||
      "node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5183 --strictPort",
    env: { RAPTOR_ADMIN_ADDR: "127.0.0.1:5183" },
    url: "http://127.0.0.1:5183",
    reuseExistingServer: false,
  },
  workers: 1,
});

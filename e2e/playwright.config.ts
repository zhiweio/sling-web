import { defineConfig } from "@playwright/test"

export default defineConfig({
  testDir: "./specs",
  timeout: 120_000,
  retries: process.env.CI ? 1 : 0,
  workers: 1, // 共享一个 server 实例，串行稳定
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: "http://127.0.0.1:5666",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    locale: "zh-CN",
  },
  outputDir: "./.artifacts",
})

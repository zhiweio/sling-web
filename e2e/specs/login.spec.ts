// Login journey on a default-credentialed deployment (no --admin-password):
// unauthenticated redirect → wrong-password error → admin/admin login →
// forced password change → workbench access → sign out → sign back in.
import { test, expect, serverBinary } from "../helpers/env"
import { spawn, type ChildProcess } from "node:child_process"
import { mkdtempSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

test.describe("login & forced password change", () => {
  let proc: ChildProcess
  let baseUrl: string
  let workdir: string

  test.beforeAll(async () => {
    // isolated instance: default credentials seeded (admin/admin), own port
    // and data dir so the shared suite server is untouched
    workdir = mkdtempSync(join(tmpdir(), "sling-e2e-login-"))
    for (let attempt = 0; attempt < 5; attempt++) {
      const port = 16200 + Math.floor(Math.random() * 200)
      baseUrl = `http://127.0.0.1:${port}`
      proc = spawn(serverBinary(), ["--port", String(port), "--workdir", workdir, "--no-open"], {
        stdio: ["ignore", "pipe", "pipe"],
      })
      const deadline = Date.now() + 30_000
      while (Date.now() < deadline) {
        if (proc.exitCode !== null) break
        try {
          const resp = await fetch(`${baseUrl}/api/health`)
          if (resp.ok) break
        } catch {}
        await new Promise((r) => setTimeout(r, 300))
      }
      if (proc.exitCode === null) return
    }
    throw new Error("isolated login-test server did not become healthy")
  })

  test.afterAll(() => {
    proc?.kill()
  })

  test("default credentials force a password change before the workbench", async ({ page }) => {
    // unauthenticated visit is routed to the login page
    await page.goto(`${baseUrl}/jobs`)
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible()

    // wrong password → inline error, no navigation
    // (exact: the show/hide toggle's aria-label contains "password")
    await page.getByLabel("Password", { exact: true }).fill("wrong-pass")
    await page.getByRole("button", { name: "Sign in" }).click()
    await expect(page.getByText("Invalid username or password")).toBeVisible()

    // default credentials are accepted but gated behind a password change
    await page.getByLabel("Password", { exact: true }).fill("admin")
    await page.getByRole("button", { name: "Sign in" }).click()

    await expect(page.getByRole("heading", { name: "Change password" })).toBeVisible()
    await expect(page.getByText(/still uses the default password/)).toBeVisible()

    // validation: the min-length rule wins while both fields are short
    await page.getByLabel("Current password").fill("admin")
    await page.getByLabel("New password", { exact: true }).fill("short")
    await page.getByLabel("Confirm new password").fill("short2")
    await expect(page.getByText("Password must be at least 8 characters")).toBeVisible()
    await page.getByLabel("New password", { exact: true }).fill("sling-pass-123")
    await page.getByLabel("Confirm new password").fill("sling-pass-123")

    await page.getByRole("button", { name: "Change password" }).click()

    // the gate lifts and the workbench renders
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible({ timeout: 15_000 })
  })

  test("new password persists; sign out returns to the login page", async ({ page }) => {
    // sign in with the password set by the previous test
    await page.goto(`${baseUrl}/login`)
    await page.getByLabel("Password", { exact: true }).fill("sling-pass-123")
    await page.getByRole("button", { name: "Sign in" }).click()
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible({ timeout: 15_000 })

    // the user menu issues a real logout: the session cookie dies and the
    // auth guard routes back to the login page
    await page.getByTitle("admin").click()
    await page.getByRole("menuitem", { name: "Sign out" }).click()
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible()
  })
})

// Test environment: launches the real sling-server binary (random port +
// temporary data directory). Auth is always on: the shared instance runs
// with a deploy-time admin password (no forced change in the main specs);
// the login journey gets its own default-credentialed instance in
// specs/login.spec.ts.
import { test as base, expect, type Page } from "@playwright/test"
import { spawn, type ChildProcess } from "node:child_process"
import { mkdtempSync, writeFileSync, mkdirSync, existsSync, copyFileSync } from "node:fs"
import { createWriteStream } from "node:fs"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

// package.json sets "type":"module" so Playwright compiles as ESM and
// __dirname does not exist; derive it from the module URL instead.
const thisDir = fileURLToPath(new URL(".", import.meta.url))

export const E2E_ADMIN_PASS = "e2e-admin-pass"
export const E2E_API_TOKEN = "e2e-api-token"

export interface SlingFixtures {
  slingUrl: string
  dataDir: string
  csvPath: string
}

let serverProc: ChildProcess | null = null
let url = ""
let dataDir = ""

// Global singleton server (workers=1 keeps tests serial)
export const test = base.extend<SlingFixtures & { page: Page }>({
  slingUrl: async ({}, use) => {
    if (!serverProc) await startServer()
    await use(url)
  },
  dataDir: async ({}, use) => {
    await use(dataDir)
  },
  csvPath: async ({ slingUrl: _u, dataDir: _d }, use) => {
    await use(join(dataDir, "orders.csv"))
  },
  // every spec starts signed in: the cookie jar of the page context holds a
  // real session issued by POST /api/auth/login
  page: async ({ page, slingUrl }, use) => {
    const resp = await page.request.post(`${slingUrl}/api/auth/login`, {
      data: { username: "admin", password: E2E_ADMIN_PASS },
    })
    if (!resp.ok()) throw new Error(`e2e login failed: ${resp.status()}`)
    await use(page)
  },
})
export { expect }

/** Resolve the sling-server binary path (Windows needs a real .exe). */
export function serverBinary(): string {
  const base = resolve(thisDir, "../../cmd/sling-server/sling-server")
  if (process.platform !== "win32") return base
  if (existsSync(base + ".exe")) return base + ".exe"
  // Node's spawn cannot launch an extensionless PE binary (CreateProcess
  // appends .exe and then fails); copy it so the suite is never
  // stale-binary dependent.
  const copy = join(mkdtempSync(join(tmpdir(), "sling-e2e-bin-")), "sling-server.exe")
  copyFileSync(base, copy)
  return copy
}

async function startServer() {
  dataDir = mkdtempSync(join(tmpdir(), "sling-e2e-"))
  mkdirSync(join(dataDir, "data"), { recursive: true })
  writeFileSync(
    join(dataDir, "orders.csv"),
    "id,name,amount,updated_at\n1,alice,100,2026-01-01\n2,bob,200,2026-01-02\n3,carol,300,2026-01-03\n",
  )

  const bin = serverBinary()

  // A random port can collide with a long-running local instance (e.g. a
  // sling-server on 5888): the spawned child would die on bind while the
  // health check could reach someone else's old process. Avoid well-known
  // port ranges + retry on child death with a new port + after health
  // passes, re-check that the child is still alive (closes the race window
  // between a failed bind and process exit).
  for (let attempt = 0; attempt < 5; attempt++) {
    const port = 15700 + Math.floor(Math.random() * 200)
    url = `http://127.0.0.1:${port}`

    serverProc = spawn(
      bin,
      [
        "--port", String(port),
        "--workdir", join(dataDir, "data"),
        "--no-open",
        "--admin-password", E2E_ADMIN_PASS,
        "--token", E2E_API_TOKEN,
      ],
      { stdio: ["ignore", "pipe", "pipe"] },
    )
    // Persist server output for debugging on failure (written to
    // dataDir/../server.log).
    const logStream = createWriteStream(join(dataDir, "server.log"), { flags: "a" })
    serverProc.stdout?.pipe(logStream)
    serverProc.stderr?.pipe(logStream)

    // wait for health
    const deadline = Date.now() + 30_000
    while (Date.now() < deadline) {
      if (serverProc.exitCode !== null) break // port taken / crashed → retry
      try {
        const resp = await fetch(`${url}/api/health`)
        if (resp.ok) {
          await new Promise((r) => setTimeout(r, 300))
          if (serverProc.exitCode === null) return // survived → truly ours
          break
        }
      } catch {}
      await new Promise((r) => setTimeout(r, 300))
    }
    serverProc?.kill()
    serverProc = null
  }
  throw new Error("sling-server did not become healthy")
}

process.on("exit", () => {
  serverProc?.kill()
})

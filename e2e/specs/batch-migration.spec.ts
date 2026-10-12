// Batch migration journey: multi-table SQLite source → tree picker →
// same-name mapping / bulk naming → per-stream execution.
// Every test idempotently creates its own connections (Playwright restarts
// the worker with a fresh server instance after a test timeout, so state
// from earlier tests cannot be relied upon).
import { test, expect, type Page } from "../helpers/env"
import { DatabaseSync } from "node:sqlite"
import { existsSync } from "node:fs"
import { join } from "node:path"

const SRC_CONN = "E2E_BATCH_SRC"
const TGT_CONN = "E2E_BATCH_TGT"

/** Idempotent setup: multi-table SQLite source DB + source/target connections */
async function ensureBatchConns(slingUrl: string, dataDir: string) {
  const srcDb = join(dataDir, "batch-src.db")
  if (!existsSync(srcDb)) {
    const db = new DatabaseSync(srcDb)
    db.exec(`
      create table orders (id integer primary key, name text, amount integer);
      insert into orders values (1,'a',10),(2,'b',20),(3,'c',30);
      create table customers (customer_id integer primary key, email text);
      insert into customers values (1,'x@y'),(2,'z@w');
      create table payments (id integer primary key, ref text);
      insert into payments values (1,'r1'),(2,'r2');
    `)
    db.close()
  }
  for (const [name, file] of [
    [SRC_CONN, "batch-src.db"],
    [TGT_CONN, "batch-tgt.db"],
  ] as const) {
    const resp = await fetch(`${slingUrl}/api/conns`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name,
        type: "sqlite",
        props: { instance: join(dataDir, file).replace(/\\/g, "/") },
      }),
    })
    if (!resp.ok && !(await resp.text()).includes("already exists")) {
      throw new Error(`create conn ${name} failed: ${resp.status}`)
    }
  }
}

/** Read the live values of all controlled inputs in the mapping/keys tables
 * (React does not write the value attribute; the property must be read). */
async function inputValues(page: Page): Promise<string[]> {
  return page.locator("input[type='text'],input:not([type])").evaluateAll((els) =>
    els.map((el) => (el as HTMLInputElement).value).filter((v) => v !== ""),
  )
}

test.describe("batch multi-table migration", () => {
  test("batch 3 tables with default same-name mapping, all streams succeed", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)
    await expect(page.getByRole("heading", { name: "New Migration Job" })).toBeVisible()

    // Step 1: tree picker grouped by schema; check 3 tables
    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("checkbox", { name: /orders/ }).first().click()
    await page.getByRole("checkbox", { name: /customers/ }).first().click()
    await page.getByRole("checkbox", { name: /payments/ }).first().click()
    await expect(page.getByText("3 stream(s) selected")).toBeVisible()
    await page.getByRole("button", { name: "Next" }).click()

    // Step 2: target connection + default same names (placeholder = source
    // full name) + all rows badged "New"
    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText("New", { exact: true }).first()).toBeVisible()
    await expect(page.getByText("3 tables").first()).toBeVisible()
    await page.getByRole("button", { name: "Next" }).click()

    // Steps 3-4: default full refresh / default advanced options
    await page.getByRole("button", { name: "Next" }).click()
    await page.getByRole("button", { name: "Next" }).click()

    // Step 5: save & run + destructive guard
    await page.getByPlaceholder("e.g. daily orders sync").fill("e2e-batch")
    await page.getByRole("button", { name: "Save + Run Now" }).click()
    const confirm = page.getByRole("alertdialog")
    await expect(confirm).toBeVisible()
    await confirm.getByRole("button", { name: "Confirm" }).click()

    // Runs page: wait for success
    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible()
    await expect(page.getByText("Success").first()).toBeVisible({ timeout: 90_000 })

    // Run detail: the streams section lists all 3 flows (object names quoted
    // per dialect, e.g. "main"."orders")
    await page.getByRole("button", { name: /e2e-batch/ }).first().click()
    const streamSection = page.locator("section").filter({ hasText: "Streams" })
    await expect(streamSection).toBeVisible({ timeout: 10_000 })
    await expect(streamSection.getByText(/orders/)).toBeVisible()
    await expect(streamSection.getByText(/customers/)).toBeVisible()
    await expect(streamSection.getByText(/payments/)).toBeVisible()
    await expect(streamSection.getByText(/7 rows|3 rows/)).toBeVisible()
  })

  test("bulk naming rules and conflict detection", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)

    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "Select all" }).click()
    await page.getByRole("button", { name: "Next" }).click()

    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })

    // Apply a prefix rule → placeholders are replaced by explicit values
    await page.getByPlaceholder("ods_", { exact: true }).fill("ods_")
    await page.getByRole("button", { name: "Apply to all" }).click()
    await expect(page.getByPlaceholder("main.orders")).toHaveCount(0)
    await expect(page.getByPlaceholder("main.ods_orders")).toHaveCount(1)
    const values = await inputValues(page)
    expect(values.filter((v) => v.startsWith("main.ods_"))).toHaveLength(3)

    // Conflict detection: rename another row to a duplicate → conflict badge
    // + Next is blocked (Playwright 1.64 removed getByDisplayValue; locate
    // the input by its row)
    const customersInput = page.locator("div.border-b").filter({ hasText: "main.customers" }).locator("input")
    await customersInput.fill("main.ods_orders")
    await expect(page.getByText("Conflict", { exact: true }).first()).toBeVisible()
    await expect(page.getByRole("button", { name: "Next" })).toBeDisabled()
    await customersInput.fill("main.ods_customers")
    await expect(page.getByRole("button", { name: "Next" })).toBeEnabled()

    await page.getByRole("button", { name: "Next" }).click()
    await page.getByRole("button", { name: "Next" }).click()
    await page.getByRole("button", { name: "Next" }).click()
    await page.getByPlaceholder("e.g. daily orders sync").fill("e2e-batch-naming")
    await page.getByRole("button", { name: "Save", exact: true }).click()
    await expect(page.getByRole("heading", { name: "Jobs" })).toBeVisible({ timeout: 10_000 })

    // Verify the saved spec via the API: all 3 streams carry explicit ods_
    // target names
    const resp = await fetch(`${slingUrl}/api/jobs?q=e2e-batch-naming`)
    const body = await resp.json()
    const job = body.jobs?.[0]
    expect(job).toBeTruthy()
    const spec = JSON.parse(job.spec_json)
    expect(spec.streams).toHaveLength(3)
    const targets = spec.streams.map((s: { target_object: string }) => s.target_object).sort()
    expect(targets).toEqual(["main.ods_customers", "main.ods_orders", "main.ods_payments"])
  })

  test("batch upsert: per-table primary keys prefilled", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)

    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "Select all" }).click()
    await page.getByRole("button", { name: "Next" }).click()

    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "Next" }).click()

    // Switch to Smart Upsert → the per-table keys section appears, prefilled
    // from the source database primary keys
    await page.getByRole("button", { name: /Smart Upsert/ }).click()
    await expect(page.getByText("Per-table keys")).toBeVisible()
    await expect
      .poll(
        async () => {
          const vals = await inputValues(page)
          return vals.includes("customer_id") && vals.filter((v) => v === "id").length >= 2
        },
        { timeout: 20_000 },
      )
      .toBe(true)
  })
})

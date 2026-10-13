// Core journey: connection creation → 5-step migration wizard → destructive
// guard → run success → visible history.
import { test, expect } from "../helpers/env"

test.describe("core migration journey", () => {
  test("connection wizard: create a local file source", async ({ page, slingUrl, dataDir }) => {
    await page.goto(`${slingUrl}/connections`)
    // exact: the empty state's "No connections" h3 shares the substring
    await expect(page.getByRole("heading", { name: "Connections", exact: true })).toBeVisible()

    await page.getByRole("button", { name: "New Connection" }).click()
    await page.getByRole("button", { name: "Local File System" }).click()
    await page.getByRole("textbox", { name: "Connection Name *" }).fill("E2E_SRC")
    await page.getByRole("textbox", { name: "path *" }).fill(dataDir.replace(/\\/g, "/"))
    await page.getByRole("button", { name: "Test Connection" }).click()

    await expect(page.getByText("Connected", { exact: false })).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: "Next" }).click()
    await page.getByRole("button", { name: "Save" }).click()

    await expect(page.getByText("E2E_SRC").first()).toBeVisible({ timeout: 10_000 })
  })

  test("connection wizard: create a SQLite target", async ({ page, slingUrl, dataDir }) => {
    await page.goto(`${slingUrl}/connections`)
    await page.getByRole("button", { name: "New Connection" }).click()
    await page.getByRole("button", { name: "SQLite" }).click()
    await page.getByRole("textbox", { name: "Connection Name *" }).fill("E2E_TGT")
    await page
      .getByRole("textbox", { name: "instance *" })
      .fill(join(dataDir, "e2e.db").replace(/\\/g, "/"))
    await page.getByRole("button", { name: "Test Connection" }).click()

    await expect(page.getByText("Connected", { exact: false })).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: "Next" }).click()
    await page.getByRole("button", { name: "Save" }).click()
    await expect(page.getByText("E2E_TGT").first()).toBeVisible({ timeout: 10_000 })
  })

  test("5-step wizard completes a migration and shows success", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/jobs/new`)
    await expect(page.getByRole("heading", { name: "New Migration Job" })).toBeVisible()

    // Step 1: source connection + file
    await page.getByRole("button", { name: /E2E_SRC/ }).click()
    await expect(page.getByText(/orders\.csv/).first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("checkbox", { name: /orders\.csv/ }).click()
    await page.getByRole("button", { name: "Next" }).click()

    // Step 2: target + table name (file streams must set an explicit target;
    // the placeholder reads "Target required")
    await page.getByRole("button", { name: /E2E_TGT/ }).click()
    const target = page.getByPlaceholder("Target required")
    await target.fill("main.orders")
    await page.getByRole("button", { name: "Next" }).click()

    // Step 3: mode (defaults to full refresh); the step description shares
    // the heading's prefix, so assert on the heading role
    await expect(page.getByRole("heading", { name: "How should data be written?" })).toBeVisible()
    await page.getByRole("button", { name: "Next" }).click()

    // Step 4: advanced (defaults)
    await page.getByRole("button", { name: "Next" }).click()

    // Step 5: name + save & run (the input has no linked label; locate by placeholder)
    await page.getByPlaceholder("e.g. daily orders sync").fill("e2e-orders")
    await page.getByRole("button", { name: "Save + Run Now" }).click()

    // destructive-operation guard
    const confirm = page.getByRole("alertdialog")
    await expect(confirm).toBeVisible()
    await expect(confirm.getByText(/dropped and recreated/)).toBeVisible()
    await confirm.getByRole("button", { name: "Confirm" }).click()

    // runs page: wait for success
    await expect(page.getByRole("heading", { name: "Runs" })).toBeVisible()
    await expect(page.getByText("Success").first()).toBeVisible({ timeout: 60_000 })
    await expect(page.getByText("3 rows").first()).toBeVisible()
  })

  test("run detail: log terminal shows the rendered migration log", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/runs`)
    await page.getByRole("button", { name: /e2e-orders/ }).first().click()
    await expect(page.getByRole("heading", { name: "Logs", exact: true })).toBeVisible()
    // The final SQL log after rendering (not the %s template); object names
    // are quoted per dialect, e.g. "main"."orders"
    await expect(page.getByText(/insert into .{0,4}main.{0,6}orders/i).first()).toBeVisible({ timeout: 20_000 })
  })

  test("overview page shows stats", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/`)
    await expect(page.getByText("Rows moved")).toBeVisible()
    // Batch cases in the suite also contribute rows; only assert the metric
    // is a non-zero number.
    await expect(page.locator("main").getByText(/^[1-9][0-9]*$/).first()).toBeVisible()
  })
})

function join(...parts: string[]) {
  return parts.join("/").replace(/\/+/g, "/").replace(/^([A-Za-z]):/, "$1:")
}

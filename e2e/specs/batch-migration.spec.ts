// 批量迁移旅程：SQLite 多表源 → 树选择 → 同名映射/批量命名 → 逐流执行
// 每个测试幂等自建连接（Playwright 在测试超时后会重启 worker 并换新 server 实例，
// 不能依赖前置测试留下的状态）。
import { test, expect, type Page } from "../helpers/env"
import { DatabaseSync } from "node:sqlite"
import { existsSync } from "node:fs"
import { join } from "node:path"

const SRC_CONN = "E2E_BATCH_SRC"
const TGT_CONN = "E2E_BATCH_TGT"

/** 幂等准备：多表 SQLite 源库 + 源/目标连接 */
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

/** 读取映射表/键表中所有受控 input 的实时值（React 不写 value 属性，须取 property） */
async function inputValues(page: Page): Promise<string[]> {
  return page.locator("input[type='text'],input:not([type])").evaluateAll((els) =>
    els.map((el) => (el as HTMLInputElement).value).filter((v) => v !== ""),
  )
}

test.describe("批量多表迁移", () => {
  test("批量 3 表默认同名迁移，逐流全部成功", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)
    await expect(page.getByRole("heading", { name: "新建迁移任务" })).toBeVisible()

    // 步骤 1：树选择器按 schema 分组，勾选 3 张表
    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("checkbox", { name: /orders/ }).first().click()
    await page.getByRole("checkbox", { name: /customers/ }).first().click()
    await page.getByRole("checkbox", { name: /payments/ }).first().click()
    await expect(page.getByText(/已选择 3 个数据流/)).toBeVisible()
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 2：目标连接 + 默认同名（占位符 = 源全名）+ 全部“新建”徽标
    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText("新建").first()).toBeVisible()
    await expect(page.getByText(/共 3 张表/)).toBeVisible()
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 3-4：默认全量覆盖 / 默认高级
    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 5：保存并运行 + 破坏性守卫
    await page.getByPlaceholder("例如：订单表每日同步").fill("e2e-batch")
    await page.getByRole("button", { name: "保存 + 立即运行" }).click()
    const confirm = page.getByRole("alertdialog")
    await expect(confirm).toBeVisible()
    await confirm.getByRole("button", { name: "确认" }).click()

    // 运行中心等待成功
    await expect(page.getByRole("heading", { name: "运行中心" })).toBeVisible()
    await expect(page.getByText("成功").first()).toBeVisible({ timeout: 90_000 })

    // 运行详情：分表明细展示 3 条流（对象名按方言渲染，如 "main"."orders"）
    await page.getByRole("button", { name: /e2e-batch/ }).first().click()
    const streamSection = page.locator("section").filter({ hasText: "分表明细" })
    await expect(streamSection).toBeVisible({ timeout: 10_000 })
    await expect(streamSection.getByText(/orders/)).toBeVisible()
    await expect(streamSection.getByText(/customers/)).toBeVisible()
    await expect(streamSection.getByText(/payments/)).toBeVisible()
    await expect(streamSection.getByText(/7 rows|3 rows/)).toBeVisible()
  })

  test("批量命名规则与冲突检测", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)

    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "全选" }).click()
    await page.getByRole("button", { name: "下一步" }).click()

    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })

    // 应用前缀规则 → 占位符被显式值替代
    await page.getByPlaceholder("ods_", { exact: true }).fill("ods_")
    await page.getByRole("button", { name: "应用到全部" }).click()
    await expect(page.getByPlaceholder("main.orders")).toHaveCount(0)
    await expect(page.getByPlaceholder("main.ods_orders")).toHaveCount(1)
    const values = await inputValues(page)
    expect(values.filter((v) => v.startsWith("main.ods_"))).toHaveLength(3)

    // 冲突检测：把另一行改成同名 → 冲突徽标 + 下一步被阻断
    // （Playwright 1.64 已移除 getByDisplayValue，按所在行定位输入框）
    const customersInput = page.locator("div.border-b").filter({ hasText: "main.customers" }).locator("input")
    await customersInput.fill("main.ods_orders")
    await expect(page.getByText("目标冲突").first()).toBeVisible()
    await expect(page.getByRole("button", { name: "下一步" })).toBeDisabled()
    await customersInput.fill("main.ods_customers")
    await expect(page.getByRole("button", { name: "下一步" })).toBeEnabled()

    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByPlaceholder("例如：订单表每日同步").fill("e2e-batch-naming")
    await page.getByRole("button", { name: "保存", exact: true }).click()
    await expect(page.getByRole("heading", { name: "迁移任务" })).toBeVisible({ timeout: 10_000 })

    // 通过 API 验证保存的 spec：3 条流均带显式 ods_ 目标名
    const resp = await fetch(`${slingUrl}/api/jobs?q=e2e-batch-naming`)
    const body = await resp.json()
    const job = body.jobs?.[0]
    expect(job).toBeTruthy()
    const spec = JSON.parse(job.spec_json)
    expect(spec.streams).toHaveLength(3)
    const targets = spec.streams.map((s: { target_object: string }) => s.target_object).sort()
    expect(targets).toEqual(["main.ods_customers", "main.ods_orders", "main.ods_payments"])
  })

  test("upsert 批量：逐表主键自动预填", async ({ page, slingUrl, dataDir }) => {
    await ensureBatchConns(slingUrl, dataDir)
    await page.goto(`${slingUrl}/jobs/new`)

    await page.getByRole("button", { name: new RegExp(SRC_CONN) }).click()
    await expect(page.getByText("main").first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "全选" }).click()
    await page.getByRole("button", { name: "下一步" }).click()

    await page.getByRole("button", { name: new RegExp(TGT_CONN) }).click()
    await expect(page.getByPlaceholder("main.orders")).toBeVisible({ timeout: 20_000 })
    await page.getByRole("button", { name: "下一步" }).click()

    // 切到智能增量更新 → 逐表主键区出现且已按源库主键预填
    await page.getByRole("button", { name: /智能增量更新/ }).click()
    await expect(page.getByText("逐表主键 / 增量字段")).toBeVisible()
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

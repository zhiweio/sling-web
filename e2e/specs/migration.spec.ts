// 核心旅程：连接创建 → 5 步迁移向导 → 破坏性守卫 → 运行成功 → 历史可见
import { test, expect } from "../helpers/env"

test.describe("核心迁移旅程", () => {
  test("连接向导：创建本地文件源", async ({ page, slingUrl, dataDir }) => {
    await page.goto(`${slingUrl}/connections`)
    await expect(page.getByRole("heading", { name: "连接管理" })).toBeVisible()

    await page.getByRole("button", { name: "新建连接" }).click()
    await page.getByRole("button", { name: "Local File System" }).click()
    await page.getByRole("textbox", { name: "连接名称 *" }).fill("E2E_SRC")
    await page.getByRole("textbox", { name: "path *" }).fill(dataDir.replace(/\\/g, "/"))
    await page.getByRole("button", { name: "测试连接" }).click()

    await expect(page.getByText("连接成功", { exact: false })).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByRole("button", { name: "保存" }).click()

    await expect(page.getByText("E2E_SRC").first()).toBeVisible({ timeout: 10_000 })
  })

  test("连接向导：创建 SQLite 目标", async ({ page, slingUrl, dataDir }) => {
    await page.goto(`${slingUrl}/connections`)
    await page.getByRole("button", { name: "新建连接" }).click()
    await page.getByRole("button", { name: "SQLite" }).click()
    await page.getByRole("textbox", { name: "连接名称 *" }).fill("E2E_TGT")
    await page
      .getByRole("textbox", { name: "instance *" })
      .fill(join(dataDir, "e2e.db").replace(/\\/g, "/"))
    await page.getByRole("button", { name: "测试连接" }).click()

    await expect(page.getByText("连接成功", { exact: false })).toBeVisible({ timeout: 15_000 })
    await page.getByRole("button", { name: "下一步" }).click()
    await page.getByRole("button", { name: "保存" }).click()
    await expect(page.getByText("E2E_TGT").first()).toBeVisible({ timeout: 10_000 })
  })

  test("5 步向导完成迁移并看到成功", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/jobs/new`)
    await expect(page.getByRole("heading", { name: "新建迁移任务" })).toBeVisible()

    // 步骤 1：源连接 + 文件
    await page.getByRole("button", { name: /E2E_SRC/ }).click()
    await expect(page.getByText(/orders\.csv/).first()).toBeVisible({ timeout: 20_000 })
    await page.getByRole("checkbox", { name: /orders\.csv/ }).click()
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 2：目标 + 表名（文件流必须显式指定目标，占位符为“需指定目标”）
    await page.getByRole("button", { name: /E2E_TGT/ }).click()
    const target = page.getByPlaceholder("需指定目标")
    await target.fill("main.orders")
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 3：模式（默认全量覆盖）
    await expect(page.getByText("数据如何写入目标？")).toBeVisible()
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 4：高级（默认）
    await page.getByRole("button", { name: "下一步" }).click()

    // 步骤 5：命名 + 保存运行（输入框无 label 关联，按占位符定位）
    await page.getByPlaceholder("例如：订单表每日同步").fill("e2e-orders")
    await page.getByRole("button", { name: "保存 + 立即运行" }).click()

    // 破坏性守卫
    const confirm = page.getByRole("alertdialog")
    await expect(confirm).toBeVisible()
    await expect(confirm.getByText(/删除并重建目标表/)).toBeVisible()
    await confirm.getByRole("button", { name: "确认" }).click()

    // 运行中心：等待成功
    await expect(page.getByRole("heading", { name: "运行中心" })).toBeVisible()
    await expect(page.getByText("成功").first()).toBeVisible({ timeout: 60_000 })
    await expect(page.getByText("3 rows").first()).toBeVisible()
  })

  test("运行详情：日志终端展示渲染后的迁移日志", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/runs`)
    await page.getByRole("button", { name: /e2e-orders/ }).first().click()
    await expect(page.getByRole("heading", { name: "日志", exact: true })).toBeVisible()
    // 渲染后的最终 SQL 日志（而非 %s 模板）；对象名按方言带引号，如 "main"."orders"
    await expect(page.getByText(/insert into .{0,4}main.{0,6}orders/i).first()).toBeVisible({ timeout: 20_000 })
  })

  test("总览页显示统计", async ({ page, slingUrl }) => {
    await page.goto(`${slingUrl}/`)
    await expect(page.getByText("搬运数据行")).toBeVisible()
    // 套件中还有批量用例贡献行数，只断言指标为非零数字
    await expect(page.locator("main").getByText(/^[1-9][0-9]*$/).first()).toBeVisible()
  })
})

function join(...parts: string[]) {
  return parts.join("/").replace(/\/+/g, "/").replace(/^([A-Za-z]):/, "$1:")
}

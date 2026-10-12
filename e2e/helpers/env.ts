// 测试环境：启动真实 sling-server 二进制（随机端口 + 临时数据目录）
import { test as base, expect } from "@playwright/test"
import { spawn, type ChildProcess } from "node:child_process"
import { mkdtempSync, writeFileSync, mkdirSync, existsSync } from "node:fs"
import { createWriteStream } from "node:fs"
import { tmpdir } from "node:os"
import { join, resolve } from "node:path"
import { fileURLToPath } from "node:url"

// package.json 为 "type":"module"，Playwright 以 ESM 编译，__dirname 不存在
const thisDir = fileURLToPath(new URL(".", import.meta.url))

export interface SlingFixtures {
  slingUrl: string
  dataDir: string
  csvPath: string
}

let serverProc: ChildProcess | null = null
let url = ""
let dataDir = ""

// 全局单例 server（workers=1 保证串行）
export const test = base.extend<SlingFixtures>({
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
})
export { expect }

async function startServer() {
  // Windows 上 Node spawn 对无扩展名路径会自动补 .exe（CreateProcess 行为），
  // 优先显式选择存在的那个，避免跑错旧构建。
  const base = resolve(thisDir, "../../cmd/sling-server/sling-server")
  const bin = process.platform === "win32" && existsSync(base + ".exe") ? base + ".exe" : base
  dataDir = mkdtempSync(join(tmpdir(), "sling-e2e-"))
  mkdirSync(join(dataDir, "data"), { recursive: true })
  writeFileSync(
    join(dataDir, "orders.csv"),
    "id,name,amount,updated_at\n1,alice,100,2026-01-01\n2,bob,200,2026-01-02\n3,carol,300,2026-01-03\n",
  )

  // 随机端口可能撞上本机常驻实例（如 5888 的 sling-server-full）：spawn 会因
  // 端口被占而退出，健康检查却可能连到别人的旧进程。避开常用区间 + 检测子进程
  // 死亡换端口重试 + 健康通过后复核进程仍存活（关掉绑定失败退出前的竞态窗口）。
  for (let attempt = 0; attempt < 5; attempt++) {
    const port = 15700 + Math.floor(Math.random() * 200)
    url = `http://127.0.0.1:${port}`

    serverProc = spawn(bin, ["--port", String(port), "--workdir", join(dataDir, "data"), "--no-open"], {
      stdio: ["ignore", "pipe", "pipe"],
    })
    // 服务端输出落盘，失败时可在临时目录排查（写入 dataDir/../server.log）
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

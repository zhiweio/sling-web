# sling-server E2E 测试(Playwright)

[English](README.md) | 简体中文

对**真实构建产物**(内嵌前端的 sling-server 单一二进制)做全栈黑盒测试:无 mock、真实 SQLite 迁移。

## 运行

```bash
just build-server   # 先构建含 UI 的二进制
just test-e2e       # 安装依赖并执行
```

或手动:

```bash
cd e2e && npm ci && npx playwright install chromium
npx playwright test
```

## 覆盖的核心旅程

| 用例 | 验证 |
|---|---|
| `specs/migration.spec.ts` | 连接向导(本地文件源、SQLite 目标)→ 5 步任务向导 → 破坏性确认守卫 → 运行 → 成功 → 历史列表;运行详情日志终端;总览统计 |
| `specs/batch-migration.spec.ts` | SQLite 多表源 → 树选择 → 同名映射/批量命名 → 冲突检测 → 逐流执行;upsert 模式逐表主键自动预填 |

fixture:测试自动启动 sling-server(随机端口 + 临时数据目录,见 `helpers/env.ts`),
并用本地 CSV → SQLite 完成真实迁移断言。

## 验收规格

`specs/acceptance.yaml` 是 Given/When/Then 验收规格(P0–P2),两种驱动方式:

- `specs/*.spec.ts` — 上面的确定性 Playwright 用例
- `ai/run-acceptance.mjs` — 验收执行器(配置 `LLM_API_KEY`/`LLM_BASE_URL` 后可由
  LLM 驱动走查),输出 JUnit 报告到 `.artifacts/`

```bash
node e2e/ai/run-acceptance.mjs [--url http://127.0.0.1:5666] [--seed]
```

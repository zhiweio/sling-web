# sling-server E2E 测试（Playwright）

对**真实构建产物**（内嵌前端的 sling-server 单一二进制）做全栈黑盒测试：无 mock、真实 SQLite 迁移。

## 运行

```bash
just build-server   # 先构建含 UI 的二进制
just test-e2e       # 安装依赖并执行
```

或手动：

```bash
cd e2e && npm ci && npx playwright install chromium
npx playwright test
```

## 覆盖的核心旅程

| 用例 | 验证 |
|---|---|
| `homepage.spec.ts` | 首页渲染、导航、统计卡 |
| `connections.spec.ts` | 连接向导 3 步（类型→表单→测试→保存）、卡片展示、Schema 浏览 |
| `migration.spec.ts` | 5 步任务向导 → 破坏性确认守卫 → 运行 → SSE 实时进度 → 成功 → 历史列表 |
| `runs.spec.ts` | 运行详情：指标、日志终端（渲染后文本）、下载 |

fixture：测试自动启动 sling-server（随机端口 + 临时数据目录），并用本地 CSV → SQLite 完成真实迁移断言。

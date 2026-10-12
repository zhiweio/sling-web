# sling-server — 数据迁移工作台

[English](README.md) | 简体中文

**sling-cli 的 Browser/Server 形态**:以 sling 引擎为核心的单二进制 Web 工作台,
面向非技术用户的"傻瓜式"数据迁移——建连接、五步向导、一键运行、实时监控。

```
┌──────────────────────────────────────────────────────┐
│  浏览器 (React SPA, 内嵌于二进制)                       │
│  总览 · 连接中心 · 5步迁移向导 · 运行中心(SSE) · 设置    │
├──────────────────────────────────────────────────────┤
│  sling-server (Go, 标准库 HTTP + SSE)                 │
│  ┌─────────┐ ┌─────────┐ ┌──────────┐ ┌───────────┐  │
│  │ 执行队列 │ │ Cron调度 │ │ 加密保险库 │ │ 版本/审计  │  │
│  └────┬────┘ └────┬────┘ └────┬─────┘ └─────┬─────┘  │
│       └───────────┴─────┬─────┴─────────────┘        │
│                   sling core 引擎 (零改动复用)          │
│                   SQLite (~/.sling/server.db, WAL)     │
└──────────────────────────────────────────────────────┘
```

## 快速开始

```bash
# 构建(前端 + Go,产出单一二进制)
just build-server

# 运行(默认 http://127.0.0.1:5666,自动打开浏览器)
./cmd/sling-server/sling-server

# 或开发模式(Vite HMR + Go 热重启,两个终端)
just web-dev    # 终端 1: Vite :5173
just dev        # 终端 2: go run -tags dev ./cmd/sling-server
```

双击运行(Windows):直接双击 `sling-server.exe`——端口被占自动递增,
若已有实例运行则直接打开浏览器指向该实例后退出(单实例体验)。

## 功能

| 模块 | 能力 |
|---|---|
| **连接中心** | 41 种数据源类型向导式添加(动态表单由引擎模板驱动)、连接测试即时反馈、Schema 树浏览、数据抽样预览、从本机 sling 环境一键导入 |
| **凭据保险库** | 密码/密钥 AES-256-GCM 加密落库(可选口令包裹主密钥,Argon2id),明文只存在于进程内存,API 永不回传 |
| **迁移向导** | 5 步:选源(表树多选/通配符/SQL) → 选目标(表名映射) → 同步方式大白话卡片(全量覆盖/清空后写入/仅追加/智能增量 upsert/快照) → 高级(列演进/重试/校验) → 调度;主键/更新键自动推导;破坏性操作双重确认 |
| **执行引擎** | FIFO 队列 + 并发 worker 池、Replication 多流有界并行、失败自动重试、连续失败自动暂停、可选行数校验 |
| **调度** | Cron + 人性化预设(每 N 分钟/每天 HH:00/每周…)、时区、暂停恢复、未来运行预览、重启自动恢复、误触发跳过 |
| **运行监控** | SSE 实时进度(行数/体积/速率/阶段/ETA)、虚拟滚动日志终端(搜索/下载)、取消、流级状态、历史多维筛选 |
| **任务管理** | 配置版本时间线 + 一键回滚、复制、YAML 导出(CLI 可直接运行)/导入、批量建任务、标签/搜索 |
| **治理** | 通知中心(站内 SSE + Webhook 渠道:企微/飞书/钉钉/Slack/通用,按事件订阅、测试发送、密钥入保险库)、审计日志(全操作留痕、多维筛选)、保留策略自动清理(运行/通知/审计/版本历史,运行时可改)、数据库快照备份(默认每日自动 + 轮换 + 下载/手动恢复) |
| **国际化** | 中英双语(默认英文,可在设置中切换中文)、深/浅色主题 |

## 安全模型

- 默认仅绑定 `127.0.0.1`;局域网部署请加 `--token <随机串>`(Bearer 鉴权)。Docker 容器内进程绑定 `0.0.0.0`(`SLING_SERVER_HOST`),`compose.yaml` 默认宿主侧端口仍只监听 `127.0.0.1`,需改 `SLING_BIND` 才对外
- 连接凭据:AES-256-GCM 加密存储于 `~/.sling/server.db`,主密钥独立存放于 `~/.sling/server.key`(0600)
- 可选 `--set-passphrase` 用口令包裹主密钥(Argon2id)
- 日志自动脱敏(复用引擎 `RedactLogLine`);API 从不返回明文密码

## CLI 参数

```
sling-server [--port 5666] [--host 127.0.0.1] [--workdir ~/.sling] [--no-open]
             [--token SECRET] [--concurrency 4] [--stream-concurrency 1]
             [--retention 90] [--set-passphrase]
sling-server version
```

## 治理配置

均可在「设置」页运行时修改(保留策略一小时内生效,备份计划立即生效):

| 设置 | 默认 | 说明 |
|---|---|---|
| `notify.on_success` | `false` | 运行成功时通知(站内 + 渠道) |
| `notify.on_failure` | `true` | 运行失败时通知 |
| `retention_days` | `90`(`--retention`) | 运行记录/日志/任务版本保留天数,`0` = 永久 |
| `retention.notifications_days` | `30` | 已读通知清理天数(未读永不清理) |
| `retention.audit_days` | `365` | 审计日志清理天数 |
| `backup.auto_enabled` | `true` | 自动快照备份开关 |
| `backup.cron` | `0 3 * * *` | 自动备份时刻(cron) |
| `backup.keep_count` | `7` | 自动备份保留份数(仅轮换自动备份) |

通知渠道(企微/飞书/钉钉/Slack/通用 Webhook)在「设置 → 通知」管理:按事件订阅、
一键测试发送;钉钉加签/飞书签名密钥 AES-256-GCM 加密存入保险库,Webhook URL 脱敏
回显、明文永不离开服务端。备份恢复:停止 sling-server → 用快照替换
`~/.sling/server.db` → 重新启动。

## 测试

```bash
just test-server            # Go 单元测试(vault/调度/日志路由/API httptest)
just test-e2e               # Playwright 全栈 E2E(真实二进制 + 真实迁移)
node e2e/ai/run-acceptance.mjs   # AI 验收(规格驱动,JUnit 报告)
```

四层质量门禁:单测 → 集成 → Playwright E2E → AI 验收(`e2e/specs/acceptance.yaml`)。

## 部署

- **单二进制**:解压即用(Windows 双击 / macOS 右键打开 / Linux 命令行)
- **Docker Compose**(服务器推荐):仓库根目录 `docker compose up -d --build`,数据持久化在 `sling-data` 卷;用 `.env` 配置(`SLING_SERVER_TOKEN`、`SLING_BIND`、`SLING_PORT`、`TZ`,见 `compose.yaml` 头部说明)
- **Docker**:`docker build -f Dockerfile.server -t sling-server . && docker run -p 5666:5666 -v sling-data:/var/lib/sling sling-server`
- **systemd**:`deploy/sling-server.service`
- **发布**:`goreleaser --snapshot --clean` 本地全平台快照;CI 原生 OS 矩阵出正式产物(`.goreleaser.yml`)

## 架构说明(开发者)

- `server/`:HTTP API + SSE + 队列 + 调度 + 保险库(**不修改 sling core**,仅通过其公开接口与 hook 接入)
- `web/`:React 19 + TS + Vite + Tailwind v4 + shadcn/ui + TanStack Query;`go:embed` 打包进二进制,`-tags dev` 反代 Vite
- 并发日志归属:goroutine-ID 路由(复用 `sling build` 模式)+ 单活动运行兜底
- 本地文件连接通过 `file://<path>/` URL 物化到进程内 `ENV_YAML` 实现根目录浏览

sling CLI(`cmd/sling`)完全未改动。

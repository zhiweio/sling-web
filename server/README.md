# sling-server — Data Migration Workbench

English | [简体中文](README.zh-CN.md)

**The Browser/Server edition of sling-cli**: a single-binary web workbench
built on the sling engine, aimed at non-technical users — create connections,
walk a five-step wizard, run, and monitor in real time.

```
┌──────────────────────────────────────────────────────────┐
│  Browser (React SPA, embedded in the binary)             │
│  Overview · Connections · 5-step wizard · Runs (SSE) ·   │
│  Settings                                                 │
├──────────────────────────────────────────────────────────┤
│  sling-server (Go, stdlib HTTP + SSE)                    │
│  ┌───────────┐ ┌───────────┐ ┌──────────┐ ┌───────────┐  │
│  │ run queue │ │ cron sched│ │ vault    │ │ versions/ │  │
│  │           │ │           │ │ (AES-GCM)│ │ audit     │  │
│  └─────┬─────┘ └─────┬─────┘ └────┬─────┘ └─────┬─────┘  │
│        └─────────────┴─────┬──────┴─────────────┘        │
│                    sling core engine (reused unmodified)  │
│                    SQLite (~/.sling/server.db, WAL)       │
└──────────────────────────────────────────────────────────┘
```

## Quick Start

```bash
# Build (frontend + Go, produces a single binary)
just build-server

# Run (defaults to http://127.0.0.1:5666, opens the browser)
./cmd/sling-server/sling-server

# Or dev mode (Vite HMR + Go, two terminals)
just web-dev    # terminal 1: Vite :5173
just dev        # terminal 2: go run -tags dev ./cmd/sling-server
```

Double-click on Windows: just double-click `sling-server.exe` — if the port
is busy it auto-increments, and if another instance is already running the
new process simply opens a browser pointing at it and exits (single-instance
behavior).

## Features

| Module | Capabilities |
|---|---|
| **Connections** | Wizard-driven setup for 41 source types (dynamic forms driven by engine templates), instant connection testing, schema tree browsing, data sampling, one-click import from the local sling environment |
| **Credential vault** | Passwords/keys encrypted at rest with AES-256-GCM (optional passphrase-wrapped master key, Argon2id); plaintext lives only in process memory and is never returned by the API |
| **Migration wizard** | 5 steps: source (table tree multi-select / wildcards / SQL) → target (table-name mapping) → sync mode in plain language (full refresh / truncate+insert / append-only / smart incremental upsert / snapshot) → advanced (column evolution / retries / validation) → schedule; primary/update keys auto-inferred; double confirmation for destructive operations |
| **Execution engine** | FIFO queue + concurrent worker pool, bounded parallelism for replication streams, automatic retries on failure, auto-pause after repeated failures, optional row-count validation |
| **Scheduling** | Cron + human presets (every N minutes / daily at HH:00 / weekly …), time zones, pause & resume, upcoming-run preview, automatic recovery after restart, missed-trigger skipping |
| **Run monitoring** | Real-time SSE progress (rows/bytes/rate/stage/ETA), virtualized log terminal (search/download), cancellation, per-stream status, history with multi-dimension filters |
| **Job management** | Config version timeline + one-click rollback, duplication, YAML export (directly runnable by the CLI) / import, bulk job creation, tags/search |
| **Governance** | Notification center (in-app SSE + webhook channels: WeCom/Feishu/DingTalk/Slack/generic; per-event subscription, test sends, secrets stored in the vault), audit log (every operation recorded, filterable), retention policies with automatic cleanup (runs/notifications/audit/version history, adjustable at runtime), database snapshot backups (daily automatic by default + rotation + download/manual restore) |
| **i18n & theming** | English/Chinese bilingual UI (English default), light/dark themes |

## Security Model

- Binds to `127.0.0.1` only by default; for LAN deployment add `--token <random-secret>` (Bearer auth). In Docker the process binds `0.0.0.0` inside the container (`SLING_SERVER_HOST`), and `compose.yaml` keeps the host-side port on `127.0.0.1` unless `SLING_BIND` is changed
- Connection credentials: AES-256-GCM encrypted in `~/.sling/server.db`; the master key lives separately in `~/.sling/server.key` (0600)
- Optional `--set-passphrase` wraps the master key with a passphrase (Argon2id)
- Logs are automatically redacted (reusing the engine's `RedactLogLine`); APIs never return plaintext passwords

## CLI Flags

```
sling-server [--port 5666] [--host 127.0.0.1] [--workdir ~/.sling] [--no-open]
             [--token SECRET] [--concurrency 4] [--stream-concurrency 1]
             [--retention 90] [--set-passphrase]
sling-server version
```

## Governance Settings

All editable at runtime on the Settings page (retention changes take effect
within an hour; backup schedule changes immediately):

| Setting | Default | Description |
|---|---|---|
| `notify.on_success` | `false` | Notify on run success (in-app + channels) |
| `notify.on_failure` | `true` | Notify on run failure |
| `retention_days` | `90` (`--retention`) | Days to keep runs/logs/job versions, `0` = forever |
| `retention.notifications_days` | `30` | Days before read notifications are pruned (unread never pruned) |
| `retention.audit_days` | `365` | Days before audit entries are pruned |
| `backup.auto_enabled` | `true` | Enable automatic snapshot backups |
| `backup.cron` | `0 3 * * *` | Automatic backup schedule (cron) |
| `backup.keep_count` | `7` | Automatic backups to keep (rotates automatic backups only) |

Notification channels (WeCom/Feishu/DingTalk/Slack/generic webhook) are
managed under Settings → Notifications: per-event subscription, one-click
test sends; DingTalk signing / Feishu signature secrets are AES-256-GCM
encrypted into the vault, webhook URLs are echoed back redacted, and
plaintext never leaves the server. Backup restore: stop sling-server →
replace `~/.sling/server.db` with the snapshot → start again.

## Testing

```bash
just test-server                  # Go unit tests (vault/scheduler/log routing/API httptest)
just test-e2e                     # full-stack Playwright E2E (real binary + real migrations)
node e2e/ai/run-acceptance.mjs    # acceptance runner (spec-driven, JUnit report)
```

Four quality gates: unit tests → integration → Playwright E2E → acceptance
specs (`e2e/specs/acceptance.yaml`).

## Deployment

- **Single binary**: unpack and run (double-click on Windows / right-click
  Open on macOS / command line on Linux)
- **Docker Compose** (recommended for servers): `docker compose up -d --build`
  from the repo root — state persists in the `sling-data` volume; configure
  via a `.env` file (`SLING_SERVER_TOKEN`, `SLING_BIND`, `SLING_PORT`, `TZ`,
  documented in `compose.yaml`)
- **Docker**: `docker build -f Dockerfile.server -t sling-server . && docker run -p 5666:5666 -v sling-data:/var/lib/sling sling-server`
- **systemd**: `deploy/sling-server.service`
- **Releases**: `goreleaser --snapshot --clean` for a local all-platform
  snapshot; CI produces official artifacts on a native OS matrix
  (`.goreleaser.yml`)

## Architecture Notes (for developers)

- `server/`: HTTP API + SSE + queue + scheduler + vault (**does not modify
  sling core**; integrates only through its public interfaces and hooks)
- `web/`: React 19 + TS + Vite + Tailwind v4 + shadcn/ui + TanStack Query;
  bundled into the binary via `go:embed`, reverse-proxied to Vite under
  `-tags dev`
- Concurrent log attribution: goroutine-ID routing (reusing the
  `sling build` approach) + a single-active-run fallback
- Local file connections are materialized into an in-process `ENV_YAML` via
  `file://<path>/` URLs to enable root-directory browsing

The sling CLI (`cmd/sling`) remains completely untouched.

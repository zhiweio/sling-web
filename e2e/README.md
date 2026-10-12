# sling-server E2E Tests (Playwright)

English | [简体中文](README.zh-CN.md)

Full-stack black-box tests against the **real built artifact** (the
sling-server single binary with the embedded frontend): no mocks, real
SQLite migrations.

## Running

```bash
just build-server   # build the binary with the embedded UI first
just test-e2e       # install dependencies and run
```

Or manually:

```bash
cd e2e && npm ci && npx playwright install chromium
npx playwright test
```

## Covered Journeys

| Spec | Verifies |
|---|---|
| `specs/migration.spec.ts` | Connection wizard (local file source, SQLite target) → 5-step migration wizard → destructive-confirmation guard → run → success → history; run detail log terminal; dashboard stats |
| `specs/batch-migration.spec.ts` | Multi-table SQLite source → tree picker → name mapping / bulk naming → conflict detection → per-stream execution; per-table primary-key prefill for upsert mode |

The suite auto-starts sling-server on a random port with a temporary data
directory (see `helpers/env.ts`) and asserts real migrations from local CSV
into SQLite.

## Acceptance Specs

`specs/acceptance.yaml` holds the Given/When/Then acceptance specification
(P0–P2). Two drivers consume it:

- `specs/*.spec.ts` — the deterministic Playwright cases above
- `ai/run-acceptance.mjs` — an acceptance runner (optionally LLM-driven via
  `LLM_API_KEY`/`LLM_BASE_URL`) that walks the spec and emits a JUnit report
  to `.artifacts/`

```bash
node e2e/ai/run-acceptance.mjs [--url http://127.0.0.1:5666] [--seed]
```

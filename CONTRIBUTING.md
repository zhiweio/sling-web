# Contributing to Sling

Thank you for considering a contribution! This document covers development
setup, project layout, testing, and the pull-request process for both parts
of this repository: the **sling CLI** and the **sling-server workbench**.

## Development Setup

Requirements:

- Go 1.26+ (https://go.dev/doc/install)
- A C compiler with `CGO_ENABLED=1` (needed for SQLite and Databricks
  Zerobus): gcc, tdm-gcc, mingw, etc.
- Node.js 20+ and [pnpm](https://pnpm.io/) (frontend work only)
- [just](https://github.com/casey/just) — the command runner used by all
  recipes below (run `just` to list them)

## Project Layout

| Path | Contents |
|---|---|
| `cmd/sling/` | CLI entrypoint |
| `cmd/sling-server/` | Workbench server entrypoint (single binary) |
| `core/sling/` | The engine: task run/read/write, replication, config, hooks |
| `core/dbio/` | Shared DB/file I/O library (`database/`, `filesys/`, `iop/`, `connection/`) — consumed by external projects, keep its public API stable |
| `server/` | Workbench HTTP layer: REST API + SSE, queue, scheduler, vault, audit, notifications |
| `web/` | React 19 + TS + Vite + Tailwind v4 SPA, embedded via `go:embed` |
| `e2e/` | Playwright black-box tests against the real built binary |
| `tests/` | CLI/engine test suites, fixtures, eval harness |

### Architecture rules for the workbench

- **sling-server must not modify sling core** (`core/sling`, `core/dbio`):
  integrate only through public interfaces and hooks. The CLI
  (`cmd/sling`) stays untouched by the workbench.
- `web/dist/` is generated — never hand-edit it; UI changes reach the
  binary only after a rebuild (or use the dev reverse proxy).

## Common Commands

```sh
just build          # build the CLI
just build-server   # build frontend + sling-server single binary
just dev            # dev mode: Go server on :5666 (pairs with `just web-dev`)
just web-dev        # Vite dev server on :5173
just web-check      # i18n key parity + TypeScript check
just test-core      # engine tests
just test-server    # workbench Go unit tests
just test-e2e       # Playwright E2E (requires `just build-server` first)
```

The dbio suites are large and gated by `-run` allowlists plus
live-credential environment variables; prefer the targeted `just test-*`
recipes over `go test ./...`. `just test-dbio-filesys-local` is the
credential-free subset.

## Testing Guidelines

When contributing, ensure your changes pass the relevant suite:

- **CLI / engine changes**: see the test overview in the
  [README](README.md#contributing) (Database, File, CLI suites) for
  targeting individual tests.
- **Workbench changes**: add or update unit tests under `server/`, and
  cover user-visible journeys in `e2e/specs/` when behavior changes. The
  E2E suite runs against a real binary with real SQLite migrations — no
  mocks.
- New features and bug fixes should come with tests whenever practical.

## Web UI Guidelines

- The UI is **bilingual**: every user-visible string must go through
  `t("key")` and be added to **both** `web/src/i18n/en.json` and
  `web/src/i18n/zh-CN.json`. `just web-check` verifies key parity.
- English is the default locale; Chinese (zh-CN) is fully supported. Do
  not hardcode UI strings in components.
- Run `cd web && pnpm run lint` (oxlint) before submitting.

## Pull Request Process

1. Make sure the code matches existing style and passes the relevant
   tests (CI runs lint, unit tests, and E2E for workbench changes).
2. Update documentation (READMEs are bilingual — update the English
   version and the Chinese counterpart `*.zh-CN.md` together) when
   interfaces or behavior change.
3. Open a pull request with a clear title and description; fill in the
   PR template.

## Reporting Issues

Use the issue templates (bug report / feature request). For security
vulnerabilities, follow [SECURITY.md](SECURITY.md) instead of opening a
public issue.

Thank you for contributing!

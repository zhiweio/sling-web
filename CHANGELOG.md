# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **sling-server workbench**: a browser-based data migration workbench built
  on the unmodified sling engine, shipped as a single binary (Go server +
  embedded React UI).
  - Connection center: guided wizards for 41 source types, connection
    testing, schema browsing, data sampling, import from the local sling
    environment.
  - Credential vault: AES-256-GCM encryption at rest, optional
    passphrase-wrapped master key (Argon2id); plaintext never leaves the
    process and is never returned by the API.
  - 5-step migration wizard with destructive-operation confirmation,
    auto-inferred primary/update keys, and batch multi-table flows.
  - Execution engine: FIFO queue with worker pool, bounded replication
    stream parallelism, retries, auto-pause on repeated failures.
  - Cron scheduling with presets, pause/resume, upcoming-run preview, and
    recovery after restart.
  - Live run monitoring over SSE (rows/bytes/rate/ETA) with a searchable,
    virtualized log terminal.
  - Governance: notification center (in-app + WeCom/Feishu/DingTalk/Slack/
    webhook channels), audit log, retention policies, snapshot backups.
  - Bilingual UI (English default, Simplified Chinese) with light/dark
    themes.
  - Deployment assets: `Dockerfile.server`, systemd unit, goreleaser config.
  - Playwright E2E suite and acceptance specs (`e2e/`).
- CI workflow (GitHub-hosted) covering workbench lint, unit tests, frontend
  build, and E2E.
- Community health files: `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md`, PR template, CODEOWNERS, Dependabot config.
- Chinese translations of the root README and component READMEs
  (`*.zh-CN.md`).

### Changed

- Workbench console messages and code comments are now English-only;
  the web UI defaults to English (user language choice persists).

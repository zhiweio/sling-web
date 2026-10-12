## What does this PR do?

<!-- A clear, concise description of the change. Mention the issue it closes, if any (e.g. "Closes #123"). -->

## Type of change

- [ ] Bug fix (non-breaking change that fixes an issue)
- [ ] New feature (non-breaking change that adds functionality)
- [ ] Breaking change (fix or feature that would cause existing behavior to change)
- [ ] Documentation update
- [ ] CI/tooling change

## Affected area

- [ ] sling CLI (`cmd/sling`, `core/`)
- [ ] Workbench server (`server/`, `cmd/sling-server/`)
- [ ] Workbench UI (`web/`)
- [ ] Tests (`tests/`, `e2e/`)
- [ ] Docs / CI

## Checklist

- [ ] Code follows the existing style; `go vet` / lint passes.
- [ ] Relevant tests were added or updated (workbench changes: `just test-server`, and `just test-e2e` for user-visible behavior).
- [ ] UI strings, if any, were added to **both** `web/src/i18n/en.json` and `web/src/i18n/zh-CN.json` (`just web-check` passes).
- [ ] Documentation updated (English + Chinese `*.zh-CN.md` counterparts kept in sync).
- [ ] sling core (`core/sling`, `core/dbio`) and the CLI remain unmodified by workbench changes.
- [ ] No plaintext credentials are logged or returned by new/changed API endpoints.

# Security Policy

## Supported Versions

Only the latest release line receives security fixes.

## Reporting a Vulnerability

Please **do not open a public issue** for security vulnerabilities. Report
them privately via [GitHub security advisories](https://github.com/slingdata-io/sling-cli/security/advisories/new)
so we can coordinate a fix and disclosure.

Include as much of the following as you can: a description of the issue, steps
to reproduce, affected versions/flags, and potential impact.

We aim to acknowledge reports within a week and will keep you informed of
remediation progress.

## sling-server Security Model

For the browser workbench, security is designed around local-first usage:

- Binds to `127.0.0.1` only by default. For LAN deployment, pass
  `--token <random-secret>` to require `Authorization: Bearer <token>` on
  every request.
- Connection credentials are encrypted at rest with AES-256-GCM in
  `~/.sling/server.db`; the master key is stored separately in
  `~/.sling/server.key` and can be wrapped with a passphrase
  (`--set-passphrase`, Argon2id).
- APIs never return plaintext credentials; logs pass through the engine's
  redaction (`RedactLogLine`). Both invariants must be preserved in
  contributions.
- Notification channel secrets (webhook signing keys) are stored encrypted
  in the same vault.

When deploying sling-server on a network, always enable `--token`, keep the
data directory (`~/.sling` by default) readable only by the service user,
and prefer running behind TLS if exposed beyond localhost.

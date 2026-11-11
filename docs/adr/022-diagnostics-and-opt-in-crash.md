# ADR-022: Diagnostics and opt-in crash reporting

Status: accepted

## Context

Users worldwide hit connect failures we cannot see in logs. Android policy
historically forbade crash reporters. We need issue tracking without
easy on-device fingerprints for censors and without putting GitHub tokens
in the client.

## Decision

- **L0:** In-memory ring of coded events (hashed endpoint ids, fail codes).
  Default logs use neutral verbs; no `vpn`/`tunnel`/`carrier`/`knock`/
  host:port in Info/Warn messages. Mobile stays Warn-only; no persisted ring.
- **L1:** User “Send diagnostics” submits a scrubbed bundle to control
  (`POST /v1/support/report`). Control opens a GitHub issue on
  `dotvpn-client` or `dotvpn-android` by platform. If control is unreachable,
  open a prefilled GitHub new-issue URL in the browser. No GitHub token on device.
- **L2:** Opt-in crash reporting only (off by default) to Sentry EU projects
  `dv-desktop` and `dv-android` (no `vpn` in slug). `SendDefaultPii` false,
  tracing off, BeforeSend scrub. DSN from release secrets / ldflags only.

## Consequences

- Desktop Settings and Android Settings gain diagnostics + crash toggles.
- Engine package stays free of Sentry; wire from cmd/desktop/mobile entrypoints.

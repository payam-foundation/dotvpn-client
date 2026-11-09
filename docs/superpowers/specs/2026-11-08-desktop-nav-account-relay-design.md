# Desktop nav, account, relay, and Home redesign

Status: implemented  
Date: 2026-11-08

## Intent

Make the desktop app a dense, usable tray client: a graphical Home to connect, pick a server, and toggle kill switch; Settings split into App / Connection / DNS; Account instead of Add invite; Relay for curated and community relays. Core stays a thin subscription store (no billing). External systems manage accounts via the admin API. Build **core and wire first**, deploy to the existing test server, then reshape the GUI against real contracts.

## Goals

- Home layout **B**: centered Hop + on/off connect switch (ink when on; lime only on Hop / Connected), ping and uptime tiles, server row (flag · name · ping), kill switch, useful connection detail without empty whitespace.
- Nav: **Home | Account | Relay | Settings**. Remove Servers, Routing, and Add-invite tabs; remove About and “Remove helper” from Settings; keep “Remove leftover blocks” for recovery/testing.
- Settings: App (login, tray, language, theme); Connection (carrier preference default Auto, routing bypass); DNS (presets + custom + optional DoH for the **existing client bypass resolver**, not tunnel DNS).
- Account: name, monthly quota remaining/limit, days to quota reset, plan end when set, max devices, add invite, app version.
- Relay: curated list read-only + community relays the user adds by link/ID; show name, country, coarse health only (no address, keys, SNI).
- Core: lean account fields for external subscription management; country on nodes/relays; client-safe account and relay list APIs; wire client sends opted-in relay IDs on token refresh.

## Non-goals

- Billing, invoices, Stripe, or plan catalogs inside core.
- End-user remote install/enroll of a relay host (decoy TLS stays on the relay machine).
- Changing how tunnelled names resolve on the node.
- Full native installers / signing / notarization in this pass.
- Exposing admin APIs or tokens to the GUI.

## Anti-goals

- Lime on the connect switch, toggles, or buttons.
- Running or installing the helper from a user-writable path.
- Showing relay or node addresses, keys, or admin health detail in the client.
- Turning core into a CRM.

## Constraints

- Design tokens and GUIDELINES.md (lime = connected state only; Hop motion; EN/FA RTL).
- Desktop stays unprivileged; helper owns secrets; IPC redacts tokens and keys (ADR-010).
- `dotvpn` CLI stays CGO-free; lab flags unchanged.
- Wire changes that are additive (`Params["cc"]`, new JSON fields) avoid a flag-day; document in a short wire ADR.
- Deploy to the operator’s existing test host after core lands. SSH material stays outside the repo (operator-provided key and host). Never commit keys, IPs as secrets, or `.pem` paths.

## Approaches considered

1. **GUI first with mocks** — fast screens, contracts rewrite later. Rejected: user wants server-testable core first.
2. **Core and wire first, then GUI (chosen)** — ship account, country, relay visibility and token `relays` wiring; deploy core to the test server; then rebuild the desktop against real types.
3. **Contract-first parallel** — same seams, UI and core in parallel. Rejected in favour of (2) because a live test server is available for core validation before UI work.

## Product decisions (locked)

| Topic | Decision |
| --- | --- |
| Home layout | B — centered switch hero |
| Connect control | On/off switch; Cancel while connecting/switching (switch pending or separate cancel) |
| Server rows | Group by node: country flag + node name; ping of best/active carrier; picker opens from Home |
| Protocols | Settings → Connection; default Auto (ladder); allow enable/disable kinds, not a second Servers tab |
| Routing | Settings → Connection (chips); no Routing tab |
| DNS | Presets (system, Cloudflare, Google, Quad9) + custom `ip:port` + optional DoH for bypass/ECH lookups only |
| Account time | Monthly quota reset; separate `expires` for plan end; “days remained” shows both when relevant |
| After `expires` | Refuse service like exhausted quota (no new tokens / sessions). Admin API can renew/extend `expires` anytime |
| Relays | Curated read-only + community via paste; safe fields only |
| Home vs relay | Server picker = nodes (country/name) only. When connected via a relay path, Home status shows that (e.g. “via RelName”); not a picker row |
| Helper UI | No uninstall in UI; keep unblock |

## Core and wire design

### Account (subscription surface)

Extend `Account` in core store (keep existing behaviour where possible):

| Field | Notes |
| --- | --- |
| `id`, `name`, `key_hash` | Existing; expose `name` to clients |
| `created` | Existing |
| `expires` | New nullable plan end |
| `quota_bytes`, `used_bytes`, `period` | Existing monthly cycle until `expires` |
| `conc` | Treat as max devices; admin may alias as `max_devices` in API |
| `mbps` | Keep |
| `disabled` | Keep; treat as immediate deny |
| `external_ref` | Optional opaque billing/CRM id; never to clients |

**Public client API:** `GET /v1/account` (Bearer account key) returning safe JSON: `name`, `quota_limit`, `quota_remaining`, `quota_reset`, `expires`, `max_devices`, `mbps`, `disabled`. Fix `HEAD /v1/quota` to refuse disabled accounts. Admin PATCH gains `expires` and `external_ref`.

External subscription systems write through the existing admin API only.

### Country

- `node.country` and `Relay.Country` as ISO-3166 alpha-2.
- Propagate into signed endpoints as `Params["cc"]` (and keep `Endpoint.Name` as the display node/relay name).
- Relays: set on create/register/admin; include in safe client relay list.

### Relays for clients

- `GET /v1/relays` (Bearer account key): curated/public relays visible to the account plus any community IDs the client will opt into after paste (validation: known id, not revoked). Fields: `id`, `name`, `country`, `tier` (`curated`|`community`), `health` (`ok`|`degraded`|`offline`). No addr/key/SNI.
- Parse `dotvpn-relay://<id>` (and accept bare id). Client config stores opted-in IDs; discovery token POST includes `"relays": [...]` (server already accepts this).
- Relay host setup remains CLI (`register` / `enroll`); desktop only opts in.

### Builder calls (settled here)

- Exit IP on Home: probe a small generate_204/ifconfig-style check through the tunnel when connected; omit the line until known. No new core field required.
- Carrier preference: settings list of enabled kinds; empty/all = Auto ladder order.
- Coarse health: map existing admin observations to ok / degraded / offline without exposing internals.
- IPC version bump when adding `Account`, `Relays`, `AddRelay`, `RemoveRelay`, and richer `Server` (`country`, `node` grouping).

## Desktop (after core is live)

1. IPC + helper: account poll, grouped servers, relay list/add/remove, DNS preset enum + DoH flag on settings.
2. Frontend: nav and screens per locked decisions; update i18n EN/FA; Playwright shots; remove dead screens.
3. Design note: amend GUIDELINES connect-button rule to allow the Home on/off switch (still never lime).

## Deploy / verify order

1. Wire types + core store/API + discovery client `relays` field.
2. Unit tests; deploy **dotvpn-core** to the operator test server (SSH outside repo).
3. Smoke: `GET /v1/account`, `GET /v1/relays`, token with community relay id, servers list shows `cc`.
4. Client helper + IPC against that server.
5. Desktop GUI redesign and e2e screenshots.

## Builder calls (settled without partner)

- **Server grouping key:** group by `Endpoint.Name` (node or relay display name). Static/CDN rows with a shared name stay in that group. Pin stores the preferred endpoint id under the selected group; Auto clears pin. Missing `cc` → no flag, name only.
- **Country source:** node reports `country` on join/heartbeat; admin can PATCH node and relay country. No GeoIP in v1. Existing rows stay empty until set.
- **`GET /v1/relays`:** always returns public curated active relays; client also sends its opted-in community ids (query `ids=`). Unknown/revoked id → omit or error on add, not a fake row. Pending community → `health: offline` (or `degraded` if partially probed). Private curated (`public=false`) only appear if their id was opted in (same path as community).
- **Relay vs Home list:** Home picker is **nodes only** (country + name = desired location). Relays are never picker rows. When the active carrier is reverse/dns, Home status shows connected-via-relay (relay display name / country), sourced from the active endpoint metadata. Relay tab remains the place to browse curated/community and paste IDs.
- **Unlimited quota:** `quota_limit == 0` means unlimited (match today’s `overQuota`); Account UI shows “Unlimited”.
- **Disabled account:** `GET /v1/account` returns 200 with `disabled: true` so the UI can explain; token and quota HEAD refuse.
- **Expired account:** same deny path as over-quota / disabled for token and connect; `GET /v1/account` still 200 with `expires` in the past so Account can say “ended” and invite renewal. Admin PATCH `expires` renews without recreating the account.
- **Home cancel:** switch shows connecting state; a quiet “Cancel” text control under it while connecting/switching.
- **Flags:** bundled SVG/emoji per design; no network fetch.
- **DoH presets:** reuse client ECH DoH pins (Cloudflare, Google, Quad9) where possible.
- **Deploy:** operator SSH; key file is outside the repo. Agent deploys core after this design is approved and during implementation (systemd/docker detected on host). SQLite migration is additive columns with defaults.
- Exact admin JSON names, observation→health mapping, and migration SQL stay with the builder.

## Open for partner

None — answers locked in the product table above. Awaiting design approval, then implementation plan.

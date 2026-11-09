# Desktop nav, account, relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship lean account/relay/country APIs on core+wire, deploy to the test server, then reshape the desktop GUI (Home B, Account, Relay, Settings) against those contracts.

**Architecture:** Approach 2 from the approved spec. Core owns subscription fields and safe client APIs; wire discovery sends opted-in relay IDs; client helper exposes redacted IPC; desktop never sees keys or relay addresses. Spec: `docs/superpowers/specs/2026-11-08-desktop-nav-account-relay-design.md`.

**Tech Stack:** Go (dotvpn-core, dotvpn-wire, dotvpn-client), SQLite control store, Wails v3 + Svelte/TS desktop, systemd deploy over SSH (key outside repo).

## Global Constraints

- Lime `#C6F432` only for connected state; Home on/off switch is ink, never lime.
- GUI unprivileged; IPC redacts tokens/keys (ADR-010).
- `dotvpn` CLI stays `CGO_ENABLED=0`; lab flags unchanged.
- Additive wire fields only (`Params["cc"]`, JSON extras); short wire ADR.
- Never commit SSH keys, host credentials, or account keys.
- Expired/disabled accounts: refuse token/connect; `GET /v1/account` still readable for UI.
- Home picker = nodes only; relay path shown in Home status when active.

---

## File map

| Area | Create / modify |
| --- | --- |
| Wire | `discovery` token request `relays`; optional account DTO types; ADR |
| Core | `Account.Expires`, `ExternalRef`; `Relay.Country` / node country; `GET /v1/account`, `GET /v1/relays`; expire+disabled enforcement; admin PATCH; `Params["cc"]` in server list / ExpandReverse |
| Client | config relay IDs; discovery call with relays; account+relay fetch; IPC types/methods; DNS preset+DoH settings; grouped servers; status via-relay |
| Desktop | nav, Home B, Account, Relay, Settings subpages; i18n; e2e; GUIDELINES connect-switch note |

---

### Task 1: Core account subscription fields + GET /v1/account

**Files:**
- Modify: `dotvpn-core/internal/control/store.go` (Account struct, migration)
- Modify: `dotvpn-core/internal/control/service.go` (overQuota / token deny for expired+disabled)
- Modify: admin account handlers + public handler
- Test: control package tests for expire deny, account GET shape, admin PATCH expires

**Produces:** `GET /v1/account` safe JSON; token refuses expired/disabled; admin can set `expires`, `external_ref`.

- [ ] **Step 1:** Write failing tests: expired account cannot mint token; `GET /v1/account` returns name, quota_*, expires, max_devices; disabled refused on HEAD quota.
- [ ] **Step 2:** Run tests — expect fail.
- [ ] **Step 3:** Add `Expires`, `ExternalRef` columns/fields; implement GET; wire deny path; admin PATCH.
- [ ] **Step 4:** Run tests — expect pass.
- [ ] **Step 5:** Commit in `dotvpn-core` with signed-off message.

### Task 2: Country on nodes and relays → Params["cc"]

**Files:**
- Modify: node config + join/heartbeat payload; Relay store; ExpandReverse / serverList
- Test: list endpoints include `cc` when set; missing cc omitted

**Produces:** ISO-3166 `cc` on signed endpoints; admin/node can set country.

- [ ] **Step 1:** Failing test that a node/relay with Country=DE yields `Params["cc"]=="DE"`.
- [ ] **Step 2:** Implement report + store + propagation.
- [ ] **Step 3:** Tests pass; commit.

### Task 3: GET /v1/relays + health coarsening

**Files:**
- Modify: `dotvpn-core/internal/control/relays.go`, `service.go`
- Test: public curated listed; `ids=` merges; revoked omitted; no addr/key in JSON

**Produces:** Client-safe relay list `{id,name,country,tier,health}`.

- [ ] **Step 1:** Failing tests for list shape and id merge.
- [ ] **Step 2:** Implement handler + observation→ok/degraded/offline.
- [ ] **Step 3:** Tests pass; commit.

### Task 4: Wire discovery sends relays + ADR

**Files:**
- Modify: `dotvpn-wire/discovery/discovery.go` token POST body
- Create: `dotvpn-wire/docs/adr/0xx-account-relay-client-surface.md` (or next number)
- Test: discovery client includes `relays` when set

**Produces:** Token request carries `"relays":[...]`; documented additive surface.

- [ ] **Step 1:** Failing test on request JSON.
- [ ] **Step 2:** Implement + ADR.
- [ ] **Step 3:** Commit; bump client `go.mod` pin when publishing tag (or go.work for local).

### Task 5: Deploy core to test server + smoke

**Files:** none in repo (deploy script optional under `dotvpn-core/deploy/` only if already patterned)

**Notes:** SSH host/key from operator env; key at `Documents/SSH/davod-hetzner.key` (not in repo). Detect systemd vs docker; additive SQLite migration; restart service.

- [ ] **Step 1:** Build linux binary; copy; migrate/restart.
- [ ] **Step 2:** Smoke with a test account key: `GET /v1/account`, `GET /v1/relays`, `POST /v1/token` with relays, servers show `cc` where set.
- [ ] **Step 3:** Record smoke results in the PR/commit message; do not commit secrets.

### Task 6: Client config + helper IPC for account, relays, richer servers, DNS

**Files:**
- Modify: `dotvpn-client/internal/config`, discovery usage, `internal/ipc/types.go`, protocol, server, client
- Modify: `internal/daemon` / settings for DNS preset, DoH, enabled carriers, relay IDs
- Test: IPC fake Session tests; config round-trip

**Produces:** IPC `Account`, `Relays`, `AddRelay`, `RemoveRelay`; `Server` with `country` + group name; `Status` via-relay fields; settings DNS/carriers; version bump.

- [ ] **Step 1:** Failing IPC/unit tests for new methods and redaction.
- [ ] **Step 2:** Implement; bump `ipc.Version`.
- [ ] **Step 3:** `go test -race` pass; commit.

### Task 7: Desktop GUI redesign

**Files:**
- Modify: `desktop/frontend/src/App.svelte`, screens, `lib/*`, i18n, e2e
- Delete/stop using: Servers/Routing/Invite tabs; helper remove; About block
- Modify: `design/GUIDELINES.md` connect control note (on/off allowed, never lime)
- Test: vitest + Playwright screenshots (Home B states, Account, Relay, Settings pages, FA/RTL)

**Produces:** Nav Home|Account|Relay|Settings; Home layout B; Settings App/Connection/DNS; working against live IPC/mock.

- [ ] **Step 1:** Restructure nav + Settings subpages + remove dead UI (mock-backed).
- [ ] **Step 2:** Home B switch, server sheet (nodes only), kill switch, via-relay status, exit-IP probe when connected.
- [ ] **Step 3:** Account + Relay screens on real IPC types.
- [ ] **Step 4:** Playwright + vitest green; commit.

### Task 8: End-to-end against test server + docs

**Files:**
- Modify: `dotvpn-client/README.md`, ADR-010 pointer if needed; core README admin fields

- [ ] **Step 1:** Point helper at test control; import invite; connect; verify Account/Relay/Home.
- [ ] **Step 2:** Update READMEs; mark spec status implemented when done.

---

## Execution notes

- Prefer subagent-driven-development per task with review gates.
- Commit per task in the owning repo (`dotvpn-wire`, `dotvpn-core`, `dotvpn-client`).
- Do not start Task 7 until Task 5 smoke is green unless blocked on deploy — then finish Task 6 against mocks only after noting the gap.

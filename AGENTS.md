# dotvpn-client

Client library and CLI. Module `github.com/payam-foundation/dotvpn-client`, binary `dotvpn`. Depends on `dotvpn-wire` only.

Contents: engine, fallback ladder, SOCKS5/HTTP proxy, TUN, fakedns, router, leak guards (Linux, macOS, Windows).

## Commands

```sh
go build ./... && go vet ./...
go test -race -count=1 ./...
```

`go.mod` has `replace github.com/payam-foundation/dotvpn-wire => ../dotvpn-wire`. CI drops it and tests the pinned wire tag and wire `main`.

## Invariants

- Kill switch defaults to deny, blackholes IPv6, drops non-DNS UDP (STUN/WebRTC leaks). Changes to `internal/leak` and `internal/tun` need per-OS reasoning in the PR.
- Client sockets are pinned to the physical interface (`SO_MARK`/bind) so the tunnel cannot capture itself.
- Local proxies bind loopback only (`loopback.Check`).
- TLS pins use `VerifyConnection`. No new `InsecureSkipVerify` unless pinned or loopback-only.
- `dotvpn -unblock` must always be able to remove the firewall rules after a crash.

## Shared rules

- Static builds: `CGO_ENABLED=0`, `-trimpath`. Docker base images pinned by digest, distroless, non-root.
- Secrets never go in the repo, unit files, env files or CI. Keys are written 0600/0640.
- Go style: stdlib first, no new dependency for what a few lines do, no placeholder code. Mark deliberate shortcuts with a `ponytail:` comment naming the ceiling and the upgrade path.
- Each repo is its own git repo and Go module. Sibling repos live next to this one; do not import across core, relay and client. Anything two of them must agree on goes in `dotvpn-wire`.

## Docs

`docs/adr/`: ADR-009. Security report: `../dotvpn-lab/docs/REPORT.md`.

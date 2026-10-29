# ADR-009: gVisor netstack for the TUN front

Status: accepted

## Context

Full tunnel without kernel-specific code per flow.

## Decision

TCP is terminated in a user-space gVisor stack fed by `wireguard/tun` devices on Linux, macOS and Windows (wintun). Each accepted flow becomes a stream on the transport, addressed by domain when fakedns issued the destination. One implementation of TCP semantics for three operating systems. Client-originated sockets are pinned to the physical interface so the tunnel cannot capture itself.

Limits: TCP and DNS only, IPv6 blackholed, gVisor pinned to a snapshot known to build with wireguard-go.

## Rejected

Per-OS packet redirection (iptables TPROXY, pf rdr, WFP callouts): three implementations, three sets of bugs.

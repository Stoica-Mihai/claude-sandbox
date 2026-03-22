# Proposal: WebSocket CORS Origin Restriction

## Summary

Replace the permissive `CheckOrigin: func(r *http.Request) bool { return true }` in the WebSocket upgrader with a same-origin check or a configurable allowlist of origins read from an environment variable.

## Motivation

The current `return true` implementation accepts WebSocket upgrade requests from any origin. This exposes the dashboard to Cross-Site WebSocket Hijacking (CSWSH): a malicious page on another origin can open a WebSocket to the dashboard and interact with terminal sessions if the user has the dashboard open in the same browser.

While the dashboard is typically accessed on `localhost`, users may expose it on a LAN or through a tunnel. Restricting the origin is a low-cost defense-in-depth measure.

## Scope

- Replace the blanket `return true` with a same-origin comparison (request `Origin` header vs. `Host` header).
- Support an optional environment variable (`ALLOWED_WS_ORIGINS`) that provides a comma-separated list of additional allowed origins.
- When the env var is unset, only same-origin requests are permitted.
- Log rejected origins at WARN level for debugging.

## Affected Files

| File | Change Type |
|------|-------------|
| `dashboard/handlers.go` | Modified — `upgrader` CheckOrigin function |
| `dashboard/main.go` | Modified — read and parse `ALLOWED_WS_ORIGINS` env var at startup |

## Risks

- Users who access the dashboard through a reverse proxy or tunnel where `Origin` and `Host` differ will see WebSocket connections rejected. The env var escape hatch mitigates this.
- Some browsers omit the `Origin` header on same-origin requests; the implementation must handle a missing `Origin` by falling back to allow (same behavior as `gorilla/websocket` default).

## Decision

Proceed with implementation.

# Spec: Web Terminal — WebSocket Security (CORS Origin)

**Spec Path:** `specs/web-terminal/spec.md`
**Change Type:** MODIFIED

---

## MODIFIED Requirements

### Requirement: WebSocket upgrade requests are restricted by origin

The WebSocket upgrader SHALL reject upgrade requests whose `Origin` header does not match the server's host or an explicitly configured allowlist, to prevent Cross-Site WebSocket Hijacking.

#### Scenario: Same-origin WebSocket request

- **WHEN** a client sends a WebSocket upgrade request
- **AND** the `Origin` header matches the `Host` header (same origin)
- **THEN** the upgrade is permitted
- **THEN** the WebSocket connection is established normally

#### Scenario: Allowed-origin WebSocket request via env var

- **WHEN** the environment variable `ALLOWED_WS_ORIGINS` is set to a comma-separated list of origins (e.g., `https://proxy.example.com,http://localhost:9090`)
- **AND** a client sends a WebSocket upgrade request with an `Origin` matching one of those values
- **THEN** the upgrade is permitted

#### Scenario: Cross-origin WebSocket request denied

- **WHEN** a client sends a WebSocket upgrade request
- **AND** the `Origin` header does not match the `Host` header
- **AND** the `Origin` is not in the `ALLOWED_WS_ORIGINS` list
- **THEN** the upgrade is rejected with HTTP 403
- **THEN** a WARN-level log entry is emitted containing the rejected origin

#### Scenario: Missing Origin header

- **WHEN** a client sends a WebSocket upgrade request without an `Origin` header
- **THEN** the upgrade is permitted (consistent with default `gorilla/websocket` behavior for non-browser clients)

#### Scenario: ALLOWED_WS_ORIGINS is unset

- **WHEN** the `ALLOWED_WS_ORIGINS` environment variable is not set or is empty
- **THEN** only same-origin requests and requests without an `Origin` header are permitted

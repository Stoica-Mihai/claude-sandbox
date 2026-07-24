# log-aggregator Specification (delta)

## MODIFIED Requirements

### Requirement: Guarded frontend exposure

The frontend SHALL proxy the log API to `logd` at both `/api/logs` and `/api/logs/` (so the bare path is not routed to the backend catch-all). A tunnel-originated request SHALL be rejected with HTTP 403 for **every** method — including GET — **unless** the host has enabled log sharing for the current share session (the frontend's `shareLogsEnabled` flag; see the share-tunnel spec), because logs may contain secrets. When the flag is set, a tunnel request SHALL be proxied like a LAN request. Non-tunnel (LAN) requests SHALL always be proxied.

#### Scenario: tunnel visitor is denied all log access by default
- **WHEN** a request to `/api/logs` (any method, including GET) originates from the share tunnel and log sharing is not enabled
- **THEN** the frontend responds 403 and does not proxy to `logd`

#### Scenario: tunnel visitor is allowed when the host opts in
- **WHEN** a request to `/api/logs` originates from the share tunnel and the host has enabled log sharing
- **THEN** the frontend proxies it verbatim to `logd` and returns the response

#### Scenario: LAN request is proxied
- **WHEN** a non-tunnel `GET /api/logs?...` reaches the frontend
- **THEN** it is proxied verbatim to `logd` and the response returned

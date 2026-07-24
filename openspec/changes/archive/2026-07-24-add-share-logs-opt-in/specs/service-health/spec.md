# service-health Specification (delta)

## MODIFIED Requirements

### Requirement: Guarded status exposure

The frontend SHALL proxy the status API to logd at both `/api/status` and `/api/status/` (so the bare path is not routed to the backend catch-all). A tunnel-originated request SHALL be rejected with 403 for **every** method — including GET — **unless** the host has enabled log sharing for the current share session (the frontend's `shareLogsEnabled` flag; see the share-tunnel spec), because status reveals fleet topology. The same flag gates status and logs together, so the Logs surface's status strip works over the tunnel exactly when log access does. When the flag is set, a tunnel request SHALL be proxied like a LAN request.

#### Scenario: tunnel visitor is denied status by default
- **WHEN** a request to `/api/status` (any method) originates from the share tunnel and log sharing is not enabled
- **THEN** the frontend responds 403 and does not proxy to logd

#### Scenario: tunnel visitor is allowed when the host opts in
- **WHEN** a request to `/api/status` originates from the share tunnel and the host has enabled log sharing
- **THEN** the frontend proxies it to logd and returns the aggregated status

#### Scenario: LAN request is proxied
- **WHEN** a non-tunnel `GET /api/status` reaches the frontend
- **THEN** it is proxied to logd and the aggregated status returned

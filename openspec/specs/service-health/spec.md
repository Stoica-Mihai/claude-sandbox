# service-health Specification

## Purpose
TBD - created by archiving change add-logd-health-phase2. Update Purpose after archive.
## Requirements
### Requirement: Shallow per-service liveness endpoint

Every service SHALL expose `GET /healthz` returning HTTP 200 when the process is up and serving, and a non-2xx (503) otherwise. The check SHALL be **shallow** — it verifies only the service's own liveness and MUST NOT probe a dependency, so it can never cascade-fail an orchestrator's restart trigger.

#### Scenario: sessiond reports its own liveness
- **WHEN** `GET /healthz` hits sessiond's internal health port and its control socket is accepting connections
- **THEN** it returns 200; if the control socket is not accepting, it returns 503 — and in neither case does it depend on any other service

#### Scenario: frontend healthz is self, not backend
- **WHEN** `GET /healthz` is requested from the frontend while the backend is unreachable
- **THEN** the frontend still returns 200 (it reports its own liveness, not backend's)

### Requirement: Synthetic health monitor

logd SHALL poll every service's `/healthz` on a fixed interval with a bounded timeout, and derive a per-service state of `up` or `down`. State changes SHALL be debounced: a service is marked `down` only after consecutive failed polls (not a single blip) and `up` again on the next successful poll. A probe error (timeout, connection refused) counts as a failed poll and MUST NOT crash the monitor.

#### Scenario: a single dropped poll does not flip state
- **WHEN** an up service fails exactly one poll then succeeds
- **THEN** its state stays `up` (no transition)

#### Scenario: sustained failure marks down, recovery marks up
- **WHEN** a service fails the configured number of consecutive polls
- **THEN** its state becomes `down` with `Since` set to the transition time; on the next successful poll it becomes `up` with `Since` updated

### Requirement: Last-log-seen tracking

logd SHALL record, per service, the time it last ingested a log line from that service, and expose it in the status. A service from which no line has been ingested SHALL report a null last-log-seen.

#### Scenario: ingesting a line updates last-log-seen
- **WHEN** logd ingests a log record for service X
- **THEN** X's last-log-seen advances to the ingest time and appears in the status response

### Requirement: Status query API

logd SHALL serve `GET /api/status` returning a JSON array of `{service, state, since, lastLogSeen}` for all monitored services, in a stable order.

#### Scenario: status lists every service with its state
- **WHEN** `GET /api/status` is requested
- **THEN** it returns one entry per monitored service, each with its current `up`/`down` state, last transition time, and last-log-seen (or null)

### Requirement: Live status stream

logd SHALL serve `GET /api/status/stream` as SSE, emitting the current status snapshot on connect and again on every state transition. A slow subscriber SHALL be evicted (bounded queue) without blocking the monitor.

#### Scenario: a transition pushes to subscribers
- **WHEN** a subscribed client is connected and a service transitions up↔down
- **THEN** the client receives an updated status snapshot reflecting the transition

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


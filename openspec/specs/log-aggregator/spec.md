# log-aggregator Specification

## Purpose
TBD - created by archiving change add-logd-logging-phase1. Update Purpose after archive.
## Requirements
### Requirement: Complete tailing with a persisted offset

`logd` SHALL tail each service log file read-only and persist a per-file byte offset (keyed by inode) to durable state, resuming from the last committed offset after a restart. Delivery is at-least-once: a crash may re-emit a few lines but MUST NOT drop any.

#### Scenario: ingestion resumes across a restart without loss
- **WHEN** `logd` has ingested up to offset X of a file, is restarted, and the file has since grown to offset Y
- **THEN** on restart `logd` resumes at X and ingests every line from X to Y (none skipped)

### Requirement: Rotation-safe, no-loss ingestion

The tailer SHALL detect rotation (inode change or size shrink) and drain the rotated generation from the last read offset before switching to the new file, keyed on the tracked inode, so no line written across a rotation is lost.

#### Scenario: lines spanning multiple rotations all surface
- **WHEN** a producer writes M lines while its file rotates several times
- **THEN** all M lines are ingested and returned by a query — none lost in a rename window

### Requirement: Partial-line integrity

The tailer SHALL never emit or count an unterminated line: it advances the persisted offset only up to the last newline and buffers the remainder until terminated. The pending buffer is bounded; an over-long unterminated line is emitted as a truncated raw record and the tailer resyncs at the next newline. A buffered partial with no forthcoming newline (e.g. a crash mid-line) is flushed as a raw record after an idle timeout.

#### Scenario: a mid-write poll does not corrupt a line
- **WHEN** a poll reads a chunk whose final line has no trailing newline
- **THEN** the completed lines are emitted and the trailing partial is buffered, emitted only once its newline arrives (or flushed as raw on idle)

#### Scenario: an unbounded line cannot exhaust memory
- **WHEN** a single line exceeds the pending-buffer cap without a newline
- **THEN** the buffered bytes are emitted as a truncated raw record and the tailer resyncs at the next newline

### Requirement: Non-JSON lines are preserved as raw

A tailed line that is not valid JSON (panic text, third-party stderr) SHALL be preserved as a `raw` record at level `error` with an ingest timestamp, never discarded.

#### Scenario: panic text becomes a queryable raw record
- **WHEN** the tailer reads a non-JSON line
- **THEN** it produces a record `{level: "error", raw: <line text>, ts: <ingest time>}` that queries can return

### Requirement: Query API over the durable files

`logd` SHALL serve `GET /api/logs` returning records newest-first from the log files (the source of truth), supporting filters `service`, `level`, `since`/`until` (RFC3339 or relative such as `-15m`), `q` (case-insensitive substring over the raw on-disk line), and `limit` (default 500, capped). The scan is bounded in time and memory (an `O(limit)` result ring; file size bounded by rotation).

#### Scenario: filtered query returns matching records newest-first
- **WHEN** a client requests `GET /api/logs?service=backend&level=error&q=timeout&limit=50`
- **THEN** it receives up to 50 backend error records whose raw line contains "timeout", ordered newest-first

### Requirement: Query completeness across rotated generations

A query SHALL scan the live file and its rotated generations (`.1 .. .N`) for each in-scope service, so a time range spanning a rotation is not silently truncated to the current file.

#### Scenario: a query spanning a rotation includes older data
- **WHEN** the requested time window includes lines now in `<service>.log.1`
- **THEN** those lines are included in the results

### Requirement: Live-tail stream

`logd` SHALL serve `GET /api/logs/stream` as SSE: it replays the recent matching tail then streams new matching lines. Each subscriber has a bounded queue that evicts on overflow. This eviction is a per-viewer view-drop and MUST NOT affect the durable store or block the tailer; every line remains retrievable via the query API.

#### Scenario: a slow subscriber is evicted without losing data or stalling ingestion
- **WHEN** an SSE subscriber cannot keep up and its queue overflows
- **THEN** lines are dropped for that subscriber only, the tailer and other subscribers are unaffected, and the dropped lines are still returned by `GET /api/logs`

### Requirement: Health endpoint

`logd` SHALL serve `GET /healthz` returning `{"status":"ok"}` for the container healthcheck.

#### Scenario: healthcheck succeeds
- **WHEN** `GET /healthz` is requested against a running `logd`
- **THEN** it returns HTTP 200 with `{"status":"ok"}`

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

### Requirement: Self-exclusion and non-critical deployment

`logd` SHALL run without `LOG_DIR` (logging to its console only) and exclude its own log file from tailing, so it never ingests itself. The dashboard SHALL remain available when `logd` is down: the frontend does not hard-depend on it and `/api/logs` returns a graceful gateway error rather than blocking startup.

#### Scenario: logd does not tail itself
- **WHEN** `logd` runs
- **THEN** it writes nothing into the shared logs volume and its tailer glob excludes any `logd*.log`

#### Scenario: dashboard survives logd being down
- **WHEN** `logd` is unreachable
- **THEN** the dashboard still loads and `/api/logs` returns a 502 error envelope rather than failing the frontend


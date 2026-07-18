## ADDED Requirements

### Requirement: Holesail sidecar owns the tunnel
A `holesail` compose service SHALL run a Node wrapper around the `holesail` npm package, exposing an HTTP control API on port 9000 reachable only on the internal `claude-net` network (no published ports). The wrapper SHALL own at most one Holesail server instance at a time, targeting `frontend:8080`, created in secure mode always. The control API SHALL be served under the exact paths `GET /api/share/status`, `POST /api/share/start`, `POST /api/share/stop`, `POST /api/share/regenerate`, plus `GET /healthz`, so the frontend's verbatim-path proxy forwards without path rewriting.

Mutating operations SHALL be serialized (a concurrent start/stop/regenerate MUST NOT race), and `start`/`stop` SHALL be idempotent. `POST /api/share/start` SHALL await readiness with a timeout strictly below the frontend proxy's 30-second client timeout, so a failed or slow DHT announce surfaces as the wrapper's own JSON error (HTTP 502 with `state:"error"` and a message), never as a bare proxy error.

#### Scenario: Status when private
- **WHEN** `GET /api/share/status` is requested and no tunnel is running
- **THEN** the wrapper SHALL respond 200 with `{"state":"private","url":null,"error":null}`

#### Scenario: Going public
- **WHEN** `POST /api/share/start` is requested while private
- **THEN** the wrapper SHALL create the Holesail instance, await readiness, and respond with `state:"public"` and the `hs://s000…` connection string as `url`

#### Scenario: Start failure surfaces as JSON
- **WHEN** the Holesail instance fails to become ready within the wrapper's timeout
- **THEN** the wrapper SHALL close the partial instance and respond 502 with `state:"error"` and a human-readable `error` message

#### Scenario: Idempotent toggles
- **WHEN** `POST /api/share/start` is requested while already public, or `POST /api/share/stop` while already private
- **THEN** the wrapper SHALL respond 200 with the current status without creating or destroying anything

### Requirement: Persistent key, private-by-default boot
The wrapper SHALL persist a 64-hex-character key at `/data/share.key` (named volume, file mode 0600), generating it with a cryptographically secure RNG when missing or invalid. Because the secure connection string is `hs://s000<key>`, the string SHALL remain stable across container restarts. `POST /api/share/regenerate` SHALL rotate the key on disk and, if a tunnel is running, restart it with the new key so the old string stops working immediately.

The wrapper SHALL always boot in the private state; public state MUST NOT be restored across restarts.

#### Scenario: String survives restart
- **WHEN** the sidecar container is restarted and the tunnel is started again
- **THEN** the connection string SHALL be identical to the one before the restart

#### Scenario: Reboot un-shares
- **WHEN** the sidecar starts (fresh boot or restart), regardless of the state before shutdown
- **THEN** the state SHALL be `private` until an explicit `POST /api/share/start`

#### Scenario: Regenerate kills the old string
- **WHEN** `POST /api/share/regenerate` is requested while public
- **THEN** the wrapper SHALL persist a new key, restart the tunnel with it, and respond with the new `url`; clients holding the old string SHALL no longer be able to connect

### Requirement: Frontend share routes with tunnel-origin guard
The frontend SHALL mirror `GET /api/share/status` and `POST /api/share/start|stop|regenerate` as per-route proxies to the sidecar (`HOLESAIL_URL`, default `http://holesail:9000`). Requests that arrived through the tunnel itself — identified by `RemoteAddr` matching an IP that the sidecar's hostname resolves to via Docker DNS — SHALL be rejected with 403 before proxying. The guard SHALL cache resolutions briefly (~10s), re-resolve once on a cache miss against a stale cache (covering a sidecar restart with a new IP), keep serving from a stale cache when resolution fails, and fail open (with a logged warning) only when the hostname has never resolved — in which case no tunnel can exist. All routes other than `/api/share/*` SHALL remain fully functional over the tunnel.

#### Scenario: Tunnel visitor cannot operate share controls
- **WHEN** a request to any `/api/share/*` route arrives with a source IP belonging to the holesail container
- **THEN** the frontend SHALL respond 403 without contacting the sidecar

#### Scenario: LAN user operates share controls
- **WHEN** a request to `/api/share/status` arrives from any other source
- **THEN** the frontend SHALL proxy it verbatim to the sidecar and return the sidecar's response unchanged

#### Scenario: Dashboard works over the tunnel
- **WHEN** a tunnel client browses the dashboard and opens a session terminal
- **THEN** pages, fragments, SSE, and WebSocket terminals SHALL work (the WS origin check passes because Origin equals Host end-to-end)

### Requirement: Share UI — globe, modal, QR
The header SHALL contain a globe icon button (stroke SVG, `iconbtn`) that opens a share `<dialog>`. While the tunnel is public, the globe SHALL render in the accent color with a small blinking live-dot (static ring under `prefers-reduced-motion`); the indicator SHALL reflect the true state on page load via a status fetch.

The modal SHALL present three states driven by `/api/share/status` responses: **private** (a security note stating that the connection string grants full dashboard access — terminals included — and must be treated like a password, plus a GO PUBLIC primary CTA), **publishing** (kit barber-pole skeleton with a `role="status"` announcement), and **public** (a QR code of the connection string, the string in a welded field+COPY row with a separate outlined regenerate button, and a GO PRIVATE ink CTA). Copy SHALL flash "COPIED ✓" and revert. Regenerate SHALL redraw the string and QR in place. Action buttons SHALL be busy-guarded against double submission, and a start failure SHALL surface the wrapper's error message in the modal hint line.

The QR SHALL be rendered on a canvas by a vendored, dependency-free QR encoder (no CDN), painting literal dark-on-paper module colors in both themes (a machine-readable artifact never themes), framed per the design-system's QR exception recorded in the `app.css` override ledger. The caption SHALL reference scanning with the Holesail Go app.

#### Scenario: Toggle to public
- **WHEN** the user clicks GO PUBLIC in the private state
- **THEN** the modal SHALL show the publishing state, then on success the public state with the QR and connection string, and the header globe SHALL switch to its live appearance

#### Scenario: Failure surfaces in the modal
- **WHEN** the start request returns `state:"error"`
- **THEN** the modal SHALL return to an actionable state showing the error message in the hint line, with GO PUBLIC re-enabled

#### Scenario: Reload reflects reality
- **WHEN** the page is reloaded while the tunnel is public
- **THEN** the initial status fetch SHALL restore the globe's live appearance without opening the modal

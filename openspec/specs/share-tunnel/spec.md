# share-tunnel Specification

## Purpose
Secure remote access to the dashboard over a Holesail peer-to-peer tunnel: a Node sidecar owns at most one tunnel and a persistent key, the frontend proxies the share-control routes and guards the mutating ones against tunnel visitors, and the Sharing settings category drives it with a QR, connection string, and an ambient "you're exposed" cue. No port forwarding, no static IP, private by default.
## Requirements
### Requirement: Holesail sidecar owns the tunnel
A `holesail` compose service SHALL run a Node wrapper around the `holesail` npm package, exposing an HTTP control API on port 9000 reachable only on the internal `claude-net` network (no published ports). The wrapper SHALL own at most one Holesail server instance at a time, created in secure mode always. Because Holesail publishes its target host to the DHT and clients use that host as their own local bind address, the wrapper SHALL point Holesail at a loopback address (`127.0.0.1`) and forward that loopback port to the frontend's tunnel listener via an in-process TCP relay, so any client — including the mobile app, which cannot override the host — can bind the published address. The control API SHALL be served under the exact paths `GET /api/share/status`, `POST /api/share/start`, `POST /api/share/stop`, `POST /api/share/regenerate`, plus `GET /healthz`, so the frontend's verbatim-path proxy forwards without path rewriting.

Mutating operations SHALL be serialized (a concurrent start/stop/regenerate MUST NOT race), and `start`/`stop` SHALL be idempotent. `POST /api/share/start` SHALL await readiness with a timeout strictly below the frontend proxy's 30-second client timeout, so a failed or slow DHT announce surfaces as the wrapper's own JSON error (HTTP 502 with `state:"error"` and a message), never as a bare proxy error. The wrapper SHALL NOT run a self-dialing liveness probe: in the loopback-relay topology a Holesail client binds the same `127.0.0.1` port the relay already owns, so such a probe can never bind and would crash the process.

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

#### Scenario: A public tunnel stays up until explicitly stopped
- **WHEN** a tunnel is public and left running
- **THEN** the wrapper SHALL keep it public until an explicit `POST /api/share/stop`/`regenerate` or a container restart — no internal timer SHALL take it down

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
The frontend SHALL mirror `GET /api/share/status` and `POST /api/share/start|stop|regenerate` as proxies to the sidecar (`HOLESAIL_URL`, default `http://holesail:9000`). Tunnel origin SHALL be a property of the listening socket, not a runtime IP/DNS heuristic: the frontend SHALL run a second, compose-internal listener (`TUNNEL_PORT`, default 8090) whose handler stamps every request as tunnel-originated, and the holesail relay SHALL target that port; requests on the main published listener are never stamped. The **mutating actions** (`POST /api/share/start|stop|regenerate`) arriving on the tunnel listener SHALL be rejected with 403 before proxying, so a tunnel visitor cannot rotate or kill the tunnel. The read-only `GET /api/share/status` SHALL always be proxied, so a client browsing over the tunnel can still see the (necessarily public) sharing state and its ambient cue. The 403 body SHALL use the sidecar's `{state,url,error}` shape so the client renders the message. All routes other than the mutating share actions SHALL remain fully functional over the tunnel.

#### Scenario: Tunnel visitor cannot mutate the tunnel
- **WHEN** a `POST /api/share/start|stop|regenerate` arrives on the tunnel listener
- **THEN** the frontend SHALL respond 403 (with a `{state,url,error}` body) without contacting the sidecar

#### Scenario: Status is readable over the tunnel
- **WHEN** a `GET /api/share/status` arrives on the tunnel listener
- **THEN** the frontend SHALL proxy it to the sidecar so the tunnel client sees the live state

#### Scenario: LAN user operates share controls
- **WHEN** a `POST /api/share/*` arrives on the main (non-tunnel) listener
- **THEN** the frontend SHALL proxy it verbatim to the sidecar and return the sidecar's response unchanged

#### Scenario: Dashboard works over the tunnel
- **WHEN** a tunnel client browses the dashboard and opens a session terminal
- **THEN** pages, fragments, SSE, and WebSocket terminals SHALL work (the WS origin check passes because Origin equals Host end-to-end)

### Requirement: Share UI — Sharing settings category, ambient glow, QR
The share controls SHALL live in the settings modal's **Sharing** category (there is no dedicated header control). While the tunnel is public, the dashboard SHALL show an ambient cue — an accent glow on the header logo mark (a soft pulse; a static glow under `prefers-reduced-motion`) — in place of a dedicated glyph. The cue SHALL reflect the true state on page load via a status fetch and update after each action. The Sharing category SHALL be disabled on mobile so a tunnel client (usually a phone) cannot disconnect itself by mis-tapping.

The Sharing panel SHALL present three states driven by `/api/share/status` responses: **private** (a security note stating that the connection string grants full dashboard access — terminals included — and must be treated like a password, plus a GO PUBLIC primary CTA), **publishing** (kit barber-pole skeleton with a `role="status"` announcement), and **public** (a QR code of the connection string, the string in a welded field+COPY row with a separate outlined regenerate button, and a GO PRIVATE ink CTA). Copy SHALL flash "COPIED ✓" and revert. Regenerate SHALL redraw the string and QR in place. Action buttons SHALL be busy-guarded against double submission, and a start failure SHALL surface the wrapper's error message in the panel's hint line.

The public state SHALL additionally present a **Share logs** toggle, off by default, that reads its initial value from `GET /api/share/logs` and writes changes via `POST /api/share/logs {"enabled":<bool>}`. Because going public resets the flag server-side, the panel SHALL re-read the value after a successful GO PUBLIC so the toggle shows off. The toggle SHALL make clear that enabling it exposes logs (which may contain secrets) to anyone holding the connection string.

The QR SHALL be rendered on a canvas by a vendored, dependency-free QR encoder (no CDN), painting literal dark-on-paper module colors in both themes (a machine-readable artifact never themes), framed per the design-system's QR exception recorded in the `app.css` override ledger. The caption SHALL reference scanning with the Holesail Go app.

#### Scenario: Toggle to public
- **WHEN** the user clicks GO PUBLIC in the Sharing panel's private state
- **THEN** the panel SHALL show the publishing state, then on success the public state with the QR and connection string, and the header logo mark SHALL start glowing

#### Scenario: Failure surfaces in the panel
- **WHEN** the start request returns `state:"error"`
- **THEN** the panel SHALL return to an actionable state showing the error message in the hint line, with GO PUBLIC re-enabled

#### Scenario: Reload reflects reality
- **WHEN** the page is reloaded while the tunnel is public
- **THEN** the initial status fetch SHALL restore the logo-mark glow without opening the settings modal

#### Scenario: Sharing is off-limits on mobile
- **WHEN** the settings modal is opened on a mobile viewport
- **THEN** the Sharing category SHALL be disabled (not selectable), leaving Session and Appearance usable

#### Scenario: Share-logs toggle defaults off and reflects the server
- **WHEN** the public state is shown after a fresh GO PUBLIC
- **THEN** the Share logs toggle SHALL read off, and toggling it on SHALL POST `{"enabled":true}` and enable log access over the tunnel

### Requirement: Opt-in log sharing over the tunnel
The frontend SHALL treat exposure of the log and status APIs over the share
tunnel as a host-controlled, off-by-default scope of the share, distinct from
the tunnel's public/private lifecycle. The frontend SHALL own an in-memory
`shareLogsEnabled` flag, initialised to `false`, which is the sole authority
consulted by the log/status tunnel guard; the holesail sidecar SHALL NOT be
involved (no sidecar change).

The flag SHALL be reset to `false` on every share lifecycle mutation
(`POST /api/share/start`, `stop`, `regenerate`), so each publish begins with
logs private; because it is in-memory, a frontend restart SHALL also leave it
`false` (fail-closed). A single flag SHALL gate **both** `/api/logs*` and
`/api/status*`.

The frontend SHALL serve two frontend-native routes, registered as specific
patterns so they take precedence over the `/api/share/` proxy prefix and are
never forwarded to the sidecar: `GET /api/share/logs` returning
`{"enabled":<bool>}` (readable from any origin, including the tunnel), and
`POST /api/share/logs` with body `{"enabled":<bool>}` that sets the flag. The
POST SHALL be rejected with 403 for a tunnel-originated request, so a tunnel
visitor cannot grant themselves log access.

#### Scenario: Logs are private by default when public
- **WHEN** the tunnel is made public and no host has enabled log sharing
- **THEN** a tunnel request to `/api/logs` or `/api/status` SHALL be rejected 403

#### Scenario: Host enables log sharing from the LAN
- **WHEN** a non-tunnel `POST /api/share/logs {"enabled":true}` is received while the tunnel is public
- **THEN** subsequent tunnel requests to `/api/logs*` and `/api/status*` SHALL be proxied (200) until the flag is reset

#### Scenario: Each publish resets to off
- **WHEN** log sharing is enabled, then the tunnel is toggled (`stop` then `start`, or `regenerate`)
- **THEN** the flag SHALL be `false` again and tunnel log/status requests SHALL be 403 until re-enabled

#### Scenario: Tunnel visitor cannot enable log sharing
- **WHEN** a `POST /api/share/logs` arrives on the tunnel listener
- **THEN** the frontend SHALL respond 403 and leave the flag unchanged

#### Scenario: Flag is readable over the tunnel
- **WHEN** a `GET /api/share/logs` arrives on the tunnel listener
- **THEN** the frontend SHALL respond 200 with the current `{"enabled":<bool>}`


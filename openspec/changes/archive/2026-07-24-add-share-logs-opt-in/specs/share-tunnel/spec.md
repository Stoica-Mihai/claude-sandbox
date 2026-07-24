# share-tunnel Specification (delta)

## ADDED Requirements

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

## MODIFIED Requirements

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

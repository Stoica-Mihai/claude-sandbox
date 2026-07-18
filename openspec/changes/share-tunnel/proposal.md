## Why

The dashboard is reachable only on localhost/LAN (`frontend` publishes `:8080` and nothing else). Accessing a running sandbox away from the machine means VPNs, port forwarding, or a static IP — none of which fit the "single `make up`" model. Holesail provides encrypted peer-to-peer tunneling with no port forwarding and no accounts: a server announces on a DHT, a client holepunches to it with a connection string. Toggling such a tunnel from the dashboard itself — with a QR code the Holesail Go mobile app can scan — makes the sandbox usable from a phone or another machine on demand, while staying private by default.

Holesail is Node-only (it sits on the holepunch/hyperdht stack, which has no Go port), so it runs as a third compose service rather than inside either Go binary.

## What Changes

- Add a `holesail` sidecar service (Node wrapper around the `holesail` npm package) exposing an HTTP control API on `:9000`, reachable only on `claude-net` (no published ports). The wrapper owns at most one Holesail server instance targeting `frontend:8080`.
  - Endpoints served under the frontend's exact route paths — `GET /api/share/status`, `POST /api/share/start`, `POST /api/share/stop`, `POST /api/share/regenerate` — so the frontend's verbatim-path `httpProxy` forwards unchanged. Plus `GET /healthz` for the compose healthcheck.
  - Always secure mode. The 64-hex key is generated once, persisted at `/data/share.key` (named volume `holesail-share-key`, mode 0600); the connection string is `hs://s000<key>`, so the string survives restarts. Regenerate rotates the key (old string dies immediately).
  - Boots PRIVATE always; public state is never restored across restarts (a host reboot un-shares by design).
- Mirror `/api/share/*` in the frontend as per-route proxies to `HOLESAIL_URL` (default `http://holesail:9000`), guarded so that the **mutating actions** (`start/stop/regenerate`) arriving **through the tunnel itself** are rejected with 403 (tunneled requests reach the frontend with source IP = the holesail container, resolved via Docker DNS and matched against `RemoteAddr`; TTL-cached, fail-open when the hostname has never resolved). The read-only `GET /api/share/status` is always proxied so a tunnel client can see the sharing state.
- Add the share UI as a category of the settings modal (no dedicated header control):
  - A **Sharing** category in the categorized settings modal (left nav Session / Appearance / Sharing) with three states: private (security warning note + GO PUBLIC), publishing (kit `.skel` barber-pole), public (QR code + connection string row with COPY and regenerate, GO PRIVATE). Disabled on mobile so a tunnel client can't mis-tap GO PRIVATE.
  - **Public state is signalled ambiently** by an accent glow on the header logo mark (`body.sharing-public`, a soft pulse; static under `prefers-reduced-motion`) — no dedicated glyph.
  - `share.js` drives it with plain fetches — status when the panel opens / on page load and after each action; no SSE (share state doesn't ride the backend broker; cross-tab staleness is an accepted non-goal).
  - QR rendered on a canvas by a vendored `qrcode-generator` (MIT, dependency-free) — no CDN. QR modules paint literal ink-on-paper hexes in both themes (machine-readable artifact; scanners want dark-on-light).

## Capabilities

### New Capabilities
- `share-tunnel`: on-demand P2P exposure of the dashboard — sidecar control API, key persistence, frontend proxy + tunnel-origin guard, and the share UI (Sharing settings category, ambient logo glow, QR).

### Modified Capabilities
<!-- None — session-api is untouched; the WS origin check already passes tunnel clients (Origin == Host holds end-to-end). The dashboard-ui settings modal gained a Sharing category to host these controls (covered by this change's Share UI requirement). -->

## Impact

- **New service:** `holesail/server.js` + `holesail/package.json` (+ lockfile), `Dockerfile.holesail`, compose service + named volume `holesail-share-key`, `Makefile` `restart-holesail`.
- **Frontend Go:** `frontend/shareguard.go` (tunnel-origin guard), `frontend/handlers.go` (four `/api/share/*` proxy routes, `Server.holesailURL`/`guard`), `frontend/main.go` (`HOLESAIL_URL` env), tests (`shareguard_test.go`, `share_proxy_test.go`, `newTestServer` update).
- **Frontend UI:** `frontend/web/templates/layout.html` (Sharing category in the settings modal, script tags), `frontend/web/static/css/app.css` (share rules + ambient logo-glow ledger entry D17 + QR hex exception D16), `frontend/web/static/js/share.js`, `frontend/web/static/js/settings.js` (category switch + mobile Sharing lockout), `frontend/web/static/vendor/qrcode-generator.js` (vendored), JS tests (`__tests__/load-share.js`, `share.test.js`).
- **APIs:** four new frontend routes, all proxied to the sidecar; no backend routes touched.
- **Security:** the connection string grants full dashboard access (terminals included) — the Sharing panel carries that warning; the mutating share actions are unreachable over the tunnel and the Sharing category is disabled on mobile; anyone with LAN dashboard access can publish (consistent with the dashboard having no auth today).
- **Dependencies:** `holesail` npm package (pinned, `npm ci` at image build is the supply-chain boundary); vendored `qrcode-generator` v1.4.4.

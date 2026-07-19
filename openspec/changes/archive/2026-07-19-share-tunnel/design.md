## Context

The dashboard publishes only `frontend:8080`. Holesail (Node-only, holepunch/hyperdht stack) can tunnel that port peer-to-peer with no port forwarding. Prior research established: a Holesail server works inside a `node:*-bookworm-slim` container on a Docker bridge network (sodium-native prebuilds included, DHT announce takes seconds); for secure mode the connection string is literally `hs://s000<key>`, so persisting the 64-hex key persists the string (`holesail` `src/index.js`); the frontend's WS origin check passes tunnel clients because Origin == Host holds end-to-end; the Holesail Go mobile app (iOS/Android) scans a QR of the `hs://` string. The UI was approved as an interactive mockup.

## Goals / Non-Goals

**Goals**
- Toggle a secure Holesail tunnel to the dashboard from the dashboard, with persistent connection string, QR, copy, and key rotation.
- Private by default: boot state is always private; publishing is an explicit act per boot.
- Share controls must not be operable by tunnel-side visitors.

**Non-Goals**
- Cross-tab/live share-state sync (no SSE; a second tab is stale until reload or action).
- Dashboard authentication (out of scope; the string is the only credential, and the modal says so).
- Per-session or per-port tunnels; UDP mode; the Holesail filemanager.

## Decisions

### Decision 1: Sidecar owns the tunnel; wrapper serves the frontend's exact paths
The wrapper (plain `node:http`, no framework) serves `GET /api/share/status`, `POST /api/share/start|stop|regenerate` directly, so the frontend reuses its verbatim-path `httpProxy` (`frontend/proxy.go`) with zero new proxy code — same pattern as every other `/api/*` per-route proxy. Toggling creates/destroys the Holesail instance inside the long-lived wrapper process; no docker.sock anywhere.

### Decision 1a: Loopback relay so the published host is bindable everywhere
`holesail-server` publishes its target `host`/`port` to the DHT, and `holesail-client` uses that **host as its own local bind address** (`holesail-client/index.js` reads `dhtData.host`). One field serves both the server's forward-target and the client's local bind. If the wrapper pointed Holesail straight at `frontend:8080`, every client — including the Holesail Go app, which can't override it — would try to bind `frontend` locally and fail (`ENOTFOUND`). So the wrapper points Holesail at `127.0.0.1:<RELAY_PORT>` (a loopback every client can bind) and runs a tiny `net` relay forwarding that loopback port to `frontend:8080`. The published host stays `127.0.0.1`; the sidecar remains a separate service (so the tunnel-origin guard's Docker-DNS resolution still works, unlike `network_mode: service:frontend`).

### Decision 2: Synchronous start with a 25s ready-timeout
`POST /api/share/start` awaits `hs.ready()` and returns the final state. The wrapper's timeout (25s) must stay under the frontend `proxyClient` timeout (30s, `proxy.go`) so a slow/failed announce surfaces as the wrapper's JSON error body (502 + `{state:"error",error}`), never as a bare proxy 502. `hs.ready()` ≈ listening; first client connect can lag a few seconds more — modal hint copy covers this.

### Decision 3: Key lifecycle
On boot: read `/data/share.key`; if missing or not 64 hex chars, generate `crypto.randomBytes(32).toString('hex')` and write with mode 0600. Regenerate rotates the key on disk and, if the tunnel is running, restarts the instance with the new key (brief downtime intended — the point is killing the old string). The key file is the credential; the named volume is not shared with any other service.

### Decision 4: State model and concurrency
`state ∈ private | publishing | public | error`, single module-level instance, all mutating ops serialized through a promise-chain mutex so concurrent start/stop/regenerate cannot race. Start when public and stop when private are idempotent no-ops returning current status.

### Decision 5: Tunnel-origin guard (frontend/shareguard.go)
Tunneled requests arrive with source IP = the holesail container. `tunnelGuard` resolves the `HOLESAIL_URL` hostname via Docker DNS (`net.LookupIP`, injected in tests), caches IPs for 10s, and 403s the request when `RemoteAddr` matches. Guard scope is **mutations only** (`start/stop/regenerate`): the read-only `GET /api/share/status` is always proxied, so a client browsing over the tunnel still sees the (necessarily public) state and its ambient glow — blocking the status read left tunnel clients showing a stale "private". The 403 body uses the sidecar's `{state,url,error}` shape so `share.js`'s `renderShare` surfaces the message. Two sharp edges handled:
- **Fail-open when the hostname has never resolved** (with `slog.Warn`): sound because an unresolvable sidecar means no tunnel exists; the proxy call 502s anyway. Fail-closed would brick share controls for LAN users whenever the sidecar is down.
- **Mismatch double-check**: a cache-miss against a cache older than ~1s forces one re-resolve before allowing — covers the sidecar restarting with a new IP mid-TTL.
The guard hooks only `handleShareProxy` (and only for non-GET there); every other route keeps working over the tunnel. As a second layer, the Sharing category is disabled on mobile (the typical tunnel client) so GO PRIVATE / regenerate can't be mis-tapped.

### Decision 6: No SSE for share state; ambient glow, not a glyph
The backend broker emits a single untyped `update` ping and the sidecar is not the backend; wiring share events through it would couple the backend to the sidecar for no real gain. `share.js` fetches `/api/share/status` on page load and when the Sharing panel opens, and after each action (responses are the final state, per Decision 2). The public/private signal is **ambient** — an accent glow on the header logo mark via a `sharing-public` body class — rather than a dedicated header glyph: on-brand (accent = live), zero extra chrome, and it survives closing the settings modal so "you're exposed" stays visible. Cross-tab/device staleness (a device already open won't update until reload/refetch) is the accepted no-SSE trade-off.

### Decision 7: QR rendering
Vendor `qrcode-generator` v1.4.4 (Kazuhiko Arase, MIT, dependency-free, global `qrcode()` factory — fits the no-bundler script-tag setup) into `static/vendor/` with a license header; CDNs are banned. `drawQR` paints modules as literal `#1a1714` on `#efe9dc` with integer module scale and a quiet zone: a machine-readable artifact never themes (scanners want dark-on-light on carbon too). Recorded in the app.css override ledger.

### Decision 8: No shared wire types
The frontend never decodes share JSON (verbatim proxy) and the browser is not a Go consumer, so `shared/types.go` gains nothing. The JSON contract lives in a header comment in `holesail/server.js` and in the spec.

### Decision 9: Compose topology
`holesail` service: build context `.` + `Dockerfile.holesail`, `claude-net` only, no published ports, healthcheck curling `:9000/healthz`, named volume `holesail-share-key:/data`, `restart: unless-stopped`. `frontend` gains `HOLESAIL_URL` + `depends_on: holesail: service_healthy`. Holesail must NOT depend on frontend (cycle); it dials `frontend:8080` lazily when a tunnel client connects. Runtime image creates and chowns `/data` to the non-root `node` user before the volume's first mount so the named volume inherits writable ownership.

## Risks / Trade-offs

- **Guard fail-open window** — accepted deliberately (see Decision 5); the alternative punishes LAN users during sidecar downtime.
- **`restart: unless-stopped` + boot-private** — a host reboot silently un-shares. Intended safe default; documented in README.
- **Supply chain** — `holesail` pulls the bare/hyper ecosystem (sodium-native etc.). Pinned version + committed lockfile; image rebuild is the only update path.
- **DHT record after stop** — `destroy()` drops the DHT node without an explicit unannounce; the stale record expires by TTL but nothing answers it. Tunnel is dead immediately; the record lingering is cosmetic.
- **Double NAT / CGNAT** — holepunching can fall back to relaying (latency) on hostile NATs; verified working on this network, but noted as environment-dependent.

## Migration Plan

Pure addition: new service + new routes + new UI. No existing behavior changes; no data migration. Rollback = remove the service and routes.

## Open Questions

None.

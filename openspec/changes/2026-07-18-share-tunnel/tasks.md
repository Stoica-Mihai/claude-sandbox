## 1. Sidecar service

- [x] 1.1 `holesail/package.json`: single pinned dependency `holesail`; `npm install` to produce `package-lock.json`; commit both.
- [x] 1.2 `holesail/server.js`: plain `node:http` control API. Env (with defaults): `SHARE_TARGET_HOST=frontend`, `SHARE_TARGET_PORT=8080`, `SHARE_CONTROL_PORT=9000`, `SHARE_KEY_FILE=/data/share.key`. Header comment documents the JSON contract.
- [x] 1.3 Key handling per design Decision 3: `loadOrCreateKey(file)` as an exported pure-ish function (readable for a later unit test) — read; validate 64 hex; else generate `crypto.randomBytes(32).toString('hex')` and write mode 0600.
- [x] 1.4 State machine per Decision 4: `state ∈ private|publishing|public|error`, `lastError`, promise-chain mutex around start/stop/regenerate.
- [x] 1.5 Endpoints: `GET /healthz` → `{"ok":true}`; `GET /api/share/status` → `{state,url,error}` (`url` non-null only when public); `POST /api/share/start` idempotent, `hs.ready()` with 25s timeout (Decision 2), failure → 502 + status JSON with `state:"error"`; `POST /api/share/stop` idempotent; `POST /api/share/regenerate` rotates key, restarts instance if running; unknown → 404 JSON.
- [x] 1.6 SIGTERM/SIGINT: `hs?.close()` then exit 0.
- [x] 1.7 `Dockerfile.holesail`: multi-stage `node:22-bookworm-slim`; builder `npm ci --omit=dev`; runtime installs `curl`, copies app + node_modules, `mkdir -p /data && chown node:node /data`, `USER node`, `CMD ["node","/app/server.js"]`.
- [x] 1.8 `docker-compose.yml`: `holesail` service per Decision 9 (claude-net, no ports, healthcheck `:9000/healthz`, `logging: *logging`, `develop.watch ./holesail/`, volume `holesail-share-key:/data`); top-level `volumes:` block.
- [x] 1.9 `Makefile`: `restart-holesail` target; add to `.PHONY`.
- [x] 1.10 Verify: `docker compose up -d --build holesail` healthy; `docker exec claude_frontend curl -s http://holesail:9000/api/share/status` → `{"state":"private",...}`; start → public with `hs://s000…` url; stop → private; regenerate → different url prefix key; `docker compose restart holesail` → private again, key file unchanged (same url after next start).

## 2. Frontend Go: routes + tunnel guard

- [x] 2.1 `frontend/shareguard.go`: `tunnelGuard` per design Decision 5 — fields `host`, `resolve func(string)([]net.IP,error)`, `ttl` (10s), mutex, `ips map[string]bool`, `expires`; constructor parses hostname from the holesail URL; `isTunnelRequest(r)` with RemoteAddr SplitHostPort + `net.ParseIP` normalization, TTL cache, stale-cache-on-resolve-failure, never-resolved fail-open with `slog.Warn`, and the ~1s mismatch double-check re-resolve.
- [x] 2.2 `frontend/handlers.go`: `Server` gains `holesailURL string` + `guard *tunnelGuard`; `NewServer` takes `holesailURL`, constructs the guard; register `GET /api/share/status`, `POST /api/share/start`, `POST /api/share/stop`, `POST /api/share/regenerate` → `s.handleShareProxy` next to the settings routes; `handleShareProxy` = guard check → 403 `{"error":"share controls are unavailable over the tunnel"}`, else `httpProxy(w, r, s.holesailURL)`.
- [x] 2.3 `frontend/main.go`: read `HOLESAIL_URL` (default `http://holesail:9000`, TrimRight `/`), pass to `NewServer`.
- [x] 2.4 `frontend/handlers_test.go`: update `newTestServer` to populate `holesailURL` and a permissive guard (resolver returning no IPs) so existing tests behave unchanged.
- [x] 2.5 `frontend/share_proxy_test.go`: verbatim forward (method+path+body reach a httptest upstream; status/body/Content-Type pass back); upstream 502 JSON passthrough; unreachable upstream → 502.
- [x] 2.6 `frontend/shareguard_test.go` (injected resolver): blocks matching IP via handler (403, upstream never called); allows non-matching; TTL caching (resolver call counts); sidecar-restart re-resolve (cache `.5`, request `.9`, resolver now `.9` → blocked); fail-open when never resolved; stale cache still blocks on resolver failure.
- [x] 2.7 `docker-compose.yml`: frontend `HOLESAIL_URL=http://holesail:9000` env + `depends_on: holesail: condition: service_healthy`.
- [x] 2.8 Verify: `go test ./...` green; with stack up, host `curl -s localhost:8080/api/share/status` → wrapper JSON.

## 3. UI markup + CSS + vendored QR

- [ ] 3.1 Vendor `qrcode-generator` v1.4.4 `qrcode.js` from the npm tarball → `frontend/web/static/vendor/qrcode-generator.min.js` with MIT license header (Decision 7). No CDN.
- [ ] 3.2 `layout.html` header: `span.share-wrap` with globe `button#shareBtn.iconbtn` (`onclick="openShareModal()"`, stroke SVG, square caps) + `i#shareDot.live-dot.hidden`, placed between the accent picker and the settings button.
- [ ] 3.3 `layout.html`: `<dialog id="shareModal">` after the settings modal — `{{template "modalBackdrop"}}`; mhead kick "Remote Access" / `SHARE.`; mbody: `#shareStatus` status line, `#statePrivate` (`.note` security warning), `#statePublishing` (`.pub-wait` + kit `.skel` + `role="status"` message), `#statePublic` (`.share-center` → `.qr-frame` + `canvas#qrCanvas` + `.qr-caption` "Scan with the Holesail Go app"; `.str-row` → welded `code#connStr` + `#copyBtn` `.btn.btn-square.btn-ink` + separate `#regenBtn` `.btn.btn-square.btn-ghost` with regen SVG); mfoot: `#shareHint` + CLOSE (`<form method="dialog">` ghost, autofocus) + `#goPublicBtn` `.btn-primary` + `#goPrivateBtn` `.btn-ink.hidden`.
- [ ] 3.4 Script tags: qrcode vendor with the other vendors; `share.js` after `settings.js`.
- [ ] 3.5 `app.css`: append share rules from the approved mockup — `.iconbtn.share-on`, `.share-wrap`/`.live-dot` (+ reduced-motion static fallback), `.share-status`, `.pub-wait`, `.share-center`, `.qr-frame`/`.qr-caption` (literal QR hexes + ledger entry), `.str-row` family (welded field+copy, ghost regen), `@media (max-width:640px)` mfoot wrap + hint hide. Do NOT re-add `.hidden` or `html,body` (exist). Tokens only elsewhere; never touch `futurism.css`.
- [ ] 3.6 Layout/style test tripwires hold: no bare `<kbd>`, still exactly 8 `keycap--mobile`, no `--ok`/`#3fb950`/`.kbd`/`.mobile-key`. `go test ./...` green.

## 4. share.js + JS tests

- [ ] 4.1 `frontend/web/static/js/share.js`: port mockup state machine (`show`, `setStates`, `copyStr`, `resetCopy`); real fetches: `openShareModal()` → `showModal()` + `refreshStatus()`; `render(st)` drives panes/buttons/globe (`share-on` class + `#shareDot`) from `st.state`, sets `#connStr` + `drawQR(st.url)` when public, error → `#shareHint` `.err` text; `goPublic()` (busy-guard `dataset.busy`, optimistic publishing pane, POST start, render response); `goPrivate()`; `regen()`; DOMContentLoaded `refreshStatus()`.
- [ ] 4.2 `drawQR(url)`: `qrcode(0,'M')`, addData, make; paint on `#qrCanvas` — `#1a1714` modules on `#efe9dc`, integer scale, quiet zone.
- [ ] 4.3 `__tests__/load-share.js`: vm-sandbox loader cloned from `load-views.js` — registered ids (shareModal with showModal/close stubs, shareBtn, shareDot, shareStatus with `<b>` child, three state panes, connStr, copyBtn with span child, regenBtn, goPublicBtn, goPrivateBtn, shareHint, qrCanvas with recording 2D-context stub), scripted `fetch`, `qrcode` global stub, clipboard spy, flushable timers.
- [ ] 4.4 `__tests__/share.test.js`: open → GET status, private render; goPublic → POST start, publishing pane in flight, public render (connStr text, qrcode received url, dot visible, share-on set); goPrivate → private render + copy reset; regen → new string + QR redraw; start 502 `{state:"error"}` → hint `.err` + message, busy cleared; copy → clipboard spy + "COPIED ✓" flash + reset after flushTimers; busy-guard → no second POST while in flight.
- [ ] 4.5 Verify: `node --test frontend/web/static/js/__tests__/` and `go test ./...` green; manual browser toggle works.

## 5. Docs + end-to-end

- [ ] 5.1 `CLAUDE.md`: third service, `/api/share/*` route map, guard + security model.
- [ ] 5.2 `README.md`: share feature section — what the string grants, private-on-boot (host reboot un-shares), Holesail Go scan flow, desktop `npx holesail <string>` alternative.
- [ ] 5.3 `.env.example`: comment block documenting the share feature (no new required vars).
- [ ] 5.4 E2E: `make up` (3 healthy); toggle public; phone (Holesail Go) scan → dashboard + working terminal over tunnel; tunnel lockout (`/api/share/status` from tunneled browser → 403, LAN → 200); regenerate drops connected client, new string works; GO PRIVATE kills tunnel; sidecar restart + full down/up → private with stable key; both test suites green.

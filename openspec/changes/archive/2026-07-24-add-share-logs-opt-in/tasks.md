# Tasks

## 1. Contract (shared/)
- [x] 1.1 Add `RouteShareLogs = "/api/share/logs"` to `shared/routes.go`
- [x] 1.2 Add `ShareLogsStatus{Enabled bool json:"enabled"}` to `shared/types.go`
- [x] 1.3 Wire-shape test pinning `{"enabled":...}` in `shared/types_test.go`

## 2. Frontend enforcement (handlers.go)
- [x] 2.1 Add `shareLogsEnabled atomic.Bool` to `Server` (default false)
- [x] 2.2 Gate: `handleGuardedLogd` rejects a tunnel request only when the flag is off
- [x] 2.3 `handleShareLogs`: GET → `{enabled}`; POST → set flag, 403 over tunnel
- [x] 2.4 Register `GET`/`POST /api/share/logs` as specific routes (beat the `/api/share/` prefix)
- [x] 2.5 Reset flag to false on `POST /api/share/start|stop|regenerate` in `handleShareProxy`

## 3. Share UI
- [x] 3.1 Add the Share-logs toggle markup to the public state in `layout.html`
- [x] 3.2 `share.js`: render toggle from `GET /api/share/logs`, write on change, re-read after go-public
- [x] 3.3 Register the toggle action in `share.js` `init()`

## 4. Verify
- [x] 4.1 `go build ./...` + `go test ./shared/...` + `npm test` (share/logs JS if covered)
- [x] 4.2 Live: over the tunnel, logs 403 by default; enable on LAN → logs 200 over tunnel; go-public resets to off
- [x] 4.3 Live: tunnel visitor cannot POST `/api/share/logs` (403)

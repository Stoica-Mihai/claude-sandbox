# Tasks: WebSocket CORS Origin Restriction

## Task List

- [x] 1.1 Add `allowedOrigins` package-level variable (`map[string]bool`) and `initAllowedOrigins()` function in `dashboard/main.go` to parse `ALLOWED_WS_ORIGINS` env var at startup
- [x] 1.2 Call `initAllowedOrigins()` in the `main()` function before the HTTP server starts
- [x] 1.3 Implement `checkOrigin(r *http.Request) bool` function in `dashboard/handlers.go` with same-origin comparison, allowlist lookup, missing-origin passthrough, and WARN logging on rejection
- [x] 1.4 Replace `CheckOrigin: func(r *http.Request) bool { return true }` in the `upgrader` with `CheckOrigin: checkOrigin`
- [x] 1.5 Write unit tests for `checkOrigin` covering: same-origin allowed, allowlist match allowed, cross-origin denied, missing origin allowed, malformed origin denied
- [x] 1.6 Write integration test verifying that a cross-origin WebSocket upgrade request receives HTTP 403
- [x] 1.7 Test with `ALLOWED_WS_ORIGINS` unset — verify only same-origin works
- [x] 1.8 Test with `ALLOWED_WS_ORIGINS` set — verify listed origins are allowed

# Design: WebSocket CORS Origin Restriction

## Overview

Replace the permissive `CheckOrigin` function in the `gorilla/websocket` upgrader with an origin validation function that compares the request's `Origin` header against the `Host` header and an optional allowlist loaded from an environment variable.

## Approach

### 1. Parse allowed origins at startup (main.go)

In `main.go`, at startup:

```go
var allowedOrigins map[string]bool

func initAllowedOrigins() {
    allowedOrigins = make(map[string]bool)
    env := os.Getenv("ALLOWED_WS_ORIGINS")
    if env == "" {
        return
    }
    for _, origin := range strings.Split(env, ",") {
        origin = strings.TrimSpace(origin)
        if origin != "" {
            allowedOrigins[origin] = true
        }
    }
}
```

Call `initAllowedOrigins()` during server initialization, before the upgrader is used.

### 2. CheckOrigin function (handlers.go)

Replace the upgrader's `CheckOrigin`:

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: checkOrigin,
}

func checkOrigin(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    if origin == "" {
        // Non-browser clients or same-origin omission — allow.
        return true
    }

    // Same-origin check: compare origin to Host.
    u, err := url.Parse(origin)
    if err != nil {
        log.Printf("WARN: rejected WebSocket upgrade — malformed Origin: %s", origin)
        return false
    }
    if u.Host == r.Host {
        return true
    }

    // Check allowlist.
    if allowedOrigins[origin] {
        return true
    }

    log.Printf("WARN: rejected WebSocket upgrade from origin %s (host: %s)", origin, r.Host)
    return false
}
```

### 3. Gorilla/websocket behavior

When `CheckOrigin` returns `false`, `gorilla/websocket` automatically returns HTTP 403 Forbidden. No additional error-handling code is needed.

## Edge Cases

- **Origin with port, Host without port:** The `url.Parse` result's `Host` field includes the port if present. The `r.Host` also includes the port. These should match naturally for `localhost:PORT` deployments.
- **Reverse proxy rewriting Host:** If a reverse proxy sets `X-Forwarded-Host`, the user should set `ALLOWED_WS_ORIGINS` to include the external origin. We do NOT read `X-Forwarded-Host` automatically to avoid header injection risks.
- **Empty string in comma list:** Trimmed and skipped during parsing.

## Testing Strategy

- Unit test `checkOrigin` with same-origin, allowed-origin, disallowed-origin, missing-origin, and malformed-origin inputs.
- Integration test with a WebSocket client setting a cross-origin `Origin` header.

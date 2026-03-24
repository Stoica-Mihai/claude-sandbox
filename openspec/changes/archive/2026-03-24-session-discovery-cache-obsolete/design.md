# Design: Session Discovery Cache with TTL

## Overview

Introduce a mutex-protected in-memory cache in `session.go` that stores the result of `discoverSessions` with a 2-second TTL. `ListSessions` checks the cache before performing filesystem I/O.

## Approach

### 1. Cache struct (session.go)

Define a package-level cache:

```go
type sessionCache struct {
    mu        sync.Mutex
    sessions  []Session
    updatedAt time.Time
    ttl       time.Duration
}

var sessCache = &sessionCache{
    ttl: 2 * time.Second,
}
```

### 2. Cache-aware ListSessions (session.go)

Modify `ListSessions` to check the cache:

```go
func ListSessions() ([]Session, error) {
    sessCache.mu.Lock()
    defer sessCache.mu.Unlock()

    if time.Since(sessCache.updatedAt) < sessCache.ttl && sessCache.sessions != nil {
        return sessCache.sessions, nil
    }

    sessions, err := discoverSessions()
    if err != nil {
        // Return stale cache if available, but still propagate the error
        if sessCache.sessions != nil {
            return sessCache.sessions, err
        }
        return nil, err
    }

    sessCache.sessions = sessions
    sessCache.updatedAt = time.Now()
    return sessions, nil
}
```

### 3. discoverSessions remains unchanged

The `discoverSessions` function continues to read `~/.claude/sessions/*.json` and parse each file. It is only called when the cache is stale or empty. No changes to its signature or logic.

### 4. Mutex strategy

A simple `sync.Mutex` is used rather than `sync.RWMutex` because:
- The critical section is short (time comparison + possible filesystem read).
- Read-heavy workloads would benefit from `RWMutex`, but the 2-second TTL means most reads hit the cache and the lock is held briefly.
- Using `Mutex` avoids the complexity of upgrading a read lock to a write lock.

If profiling later shows contention, this can be upgraded to `RWMutex` with a double-check pattern or `singleflight`.

### 5. Error handling

When `discoverSessions` fails:
- If there is existing cached data, return the stale data along with the error. This lets callers decide whether to use stale data or propagate the error.
- The cache timestamp is NOT updated, so the next call will retry.
- If there is no cached data (cold start failure), return `nil` and the error.

## Edge Cases

- **Process restart:** Cache starts empty. The first request incurs the full filesystem cost.
- **Session directory does not exist:** `discoverSessions` returns an error. Cache remains empty. Subsequent calls retry.
- **Clock skew:** `time.Since` uses the monotonic clock, so wall-clock adjustments do not affect TTL behavior.
- **Very fast session creation:** A session created immediately after a cache refresh may not appear for up to 2 seconds. This is acceptable.

## Testing Strategy

- Unit test: call `ListSessions` twice within 2 seconds, verify `discoverSessions` is called only once (mock or count calls).
- Unit test: wait >2 seconds, call again, verify refresh occurs.
- Unit test: simulate `discoverSessions` error, verify stale data is returned.
- Benchmark: compare latency of `ListSessions` with and without cache under 100+ session files.

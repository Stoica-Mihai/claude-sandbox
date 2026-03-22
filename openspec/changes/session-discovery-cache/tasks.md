# Tasks: Session Discovery Cache with TTL

## Task List

- [ ] 1.1 Define `sessionCache` struct in `dashboard/session.go` with `sync.Mutex`, `sessions []Session`, `updatedAt time.Time`, and `ttl time.Duration` fields
- [ ] 1.2 Initialize package-level `sessCache` variable with 2-second TTL
- [ ] 1.3 Modify `ListSessions` to acquire the mutex, check cache freshness via `time.Since(sessCache.updatedAt) < sessCache.ttl`, and return cached data if valid
- [ ] 1.4 On cache miss or expiry, call `discoverSessions()`, store result and timestamp in cache, then return
- [ ] 1.5 Handle `discoverSessions` error: preserve stale cache if available, do not update timestamp, propagate error to caller
- [ ] 2.1 Write unit test: two calls within 2 seconds — verify `discoverSessions` executes only once
- [ ] 2.2 Write unit test: call after 2+ seconds — verify `discoverSessions` executes again and cache is refreshed
- [ ] 2.3 Write unit test: simulate `discoverSessions` error — verify stale cached data is returned alongside the error
- [ ] 2.4 Write unit test: concurrent goroutines calling `ListSessions` — verify no data race (run with `-race` flag)
- [ ] 2.5 Benchmark `ListSessions` with 50+ session files — compare cached vs. uncached latency

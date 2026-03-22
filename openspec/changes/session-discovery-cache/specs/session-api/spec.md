# Spec: Session API — Session Discovery Performance (Cache)

**Spec Path:** `specs/session-api/spec.md`
**Change Type:** MODIFIED

---

## MODIFIED Requirements

### Requirement: Session discovery results are cached with a short TTL

The session discovery mechanism SHALL cache its results in memory with a 2-second TTL to reduce redundant filesystem reads, while maintaining near-real-time accuracy for the session list.

#### Scenario: First request populates the cache

- **WHEN** `ListSessions` is called
- **AND** the session discovery cache is empty (cold start)
- **THEN** `discoverSessions` reads `~/.claude/sessions/*.json` from the filesystem
- **THEN** the result is stored in the cache with a timestamp
- **THEN** the result is returned to the caller

#### Scenario: Subsequent request within TTL returns cached data

- **WHEN** `ListSessions` is called
- **AND** the cache was populated less than 2 seconds ago
- **THEN** `discoverSessions` is NOT called
- **THEN** the cached result is returned immediately

#### Scenario: Request after TTL expiry refreshes the cache

- **WHEN** `ListSessions` is called
- **AND** the cache was populated 2 or more seconds ago
- **THEN** `discoverSessions` is called to read fresh data from the filesystem
- **THEN** the cache is updated with the new result and timestamp
- **THEN** the new result is returned to the caller

#### Scenario: Concurrent requests during cache refresh

- **WHEN** multiple goroutines call `ListSessions` simultaneously
- **AND** the cache has expired
- **THEN** only one goroutine performs the filesystem read (mutex-protected)
- **THEN** all goroutines receive the same refreshed result
- **THEN** no data races occur

#### Scenario: discoverSessions returns an error

- **WHEN** `discoverSessions` fails (e.g., directory unreadable)
- **THEN** the cache is NOT updated (stale data is preserved if available)
- **THEN** the error is returned to the caller
- **THEN** the next call to `ListSessions` will retry `discoverSessions`

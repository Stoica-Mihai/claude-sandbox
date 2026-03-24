# Proposal: Session Discovery Cache with TTL

## Summary

Cache the results of session discovery (`discoverSessions`) with a 2-second TTL to avoid reading `~/.claude/sessions/*.json` files on every API request. The cache is invalidated after 2 seconds, ensuring near-real-time accuracy while dramatically reducing filesystem I/O.

## Motivation

The `ListSessions` function calls `discoverSessions` on every invocation, which reads and parses all JSON files in `~/.claude/sessions/`. As the number of sessions grows, this becomes a performance bottleneck:

- Each API call to list sessions triggers a full directory scan + file reads.
- On systems with slow disks (NFS, spinning drives) or many session files, this adds noticeable latency.
- The session list rarely changes faster than once per second, making aggressive caching safe.

A 2-second TTL provides a good balance between freshness and performance.

## Scope

- Add an in-memory cache (struct with mutex, cached result, and expiry timestamp) for `discoverSessions` output.
- Modify `ListSessions` to check the cache before calling `discoverSessions`.
- If the cache is valid (age < 2 seconds), return the cached result.
- If the cache is stale or empty, call `discoverSessions`, store the result, and return it.
- The cache is process-local (no external dependencies).

## Affected Files

| File | Change Type |
|------|-------------|
| `dashboard/session.go` | Modified — `ListSessions` and `discoverSessions` gain caching layer |

## Risks

- **Stale data window:** A session created or destroyed within the 2-second TTL window will not appear/disappear in the list until the cache expires. This is acceptable for the dashboard use case.
- **Concurrency:** The cache must be protected by a mutex since `ListSessions` can be called from multiple HTTP handler goroutines concurrently.

## Decision

Proceed with implementation.

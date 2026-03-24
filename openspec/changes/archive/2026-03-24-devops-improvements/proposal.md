## Why

The current Docker setup has three pain points:

1. **Slow dev cycle.** The Dockerfile builds the Go dashboard binary inline in a single stage. Every code change requires a full `docker compose build` and container restart. There is no way to iterate on dashboard code without rebuilding the entire image.

2. **Large image.** The single-stage build leaves the full Go toolchain (~600 MB) in the final image even though it is only needed at compile time. The resulting image is much larger than necessary for runtime.

3. **Inflexible dashboard port.** The dashboard listens on `:8080` and docker-compose.yml hardcodes the port mapping `8080:8080`. If port 8080 is already in use on the host, the user must manually edit docker-compose.yml. The Go binary accepts an `-addr` flag, but there is no way to configure the port via environment variable without overriding the command.

## What Changes

- Rewrite the Dockerfile as a multi-stage build: a `builder` stage (with Go, gcc, libc6-dev) compiles the binary, and a slim `runtime` stage copies only the binary and runtime dependencies (including tmux)
- Add a `docker-compose.dev.yml` file for development that targets the builder stage, volume-mounts `dashboard/` source, compiles on startup, and enables iteration without image rebuilds. Invoked via `make dev` (NOT auto-merged like an override file)
- Read a `DASHBOARD_PORT` environment variable in `dashboard/main.go` with validation, fallback to `-addr` flag, and use `${DASHBOARD_PORT:-8080}` in docker-compose.yml for the port mapping

## Capabilities

### New Capabilities
- `dev-workflow`: Multi-stage Dockerfile, `docker-compose.dev.yml` for local development with source volume mount and in-container compilation, configurable `DASHBOARD_PORT` env var

### Modified Capabilities

## Impact

- **Dockerfile**: Rewritten as multi-stage build. Builder stage installs Go and compiles the dashboard binary. Runtime stage (`debian:bookworm-slim`) copies only the binary and installs runtime packages (including tmux). `gcc`/`libc6-dev` only in builder. Net image size reduction.
- **docker-compose.yml**: Port mapping changes from `"8080:8080"` to `"${DASHBOARD_PORT:-8080}:${DASHBOARD_PORT:-8080}"`. `DASHBOARD_PORT` added to environment section.
- **docker-compose.dev.yml**: New file. Targets builder stage (has Go), mounts `./dashboard/` into the container, overrides command to compile and run from source. Invoked via `make dev`.
- **dashboard/main.go**: Reads `DASHBOARD_PORT` env var with validation (strip leading colon, reject non-numeric). Precedence: `-addr` flag > `DASHBOARD_PORT` > default `:8080`.
- **.env.example**: Add `DASHBOARD_PORT=8080` entry.
- **generate-env.sh**: Verify it propagates new `.env.example` variables correctly.
- **Makefile**: Add `dev` target that invokes `docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build`.

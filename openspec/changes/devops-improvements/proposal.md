## Why

The current Docker setup has three pain points:

1. **Slow dev cycle.** The Dockerfile builds the Go dashboard binary inline in a single stage, copies source into the image, compiles, then deletes the source. Every code change requires a full `docker compose build` and container restart. There is no way to iterate on dashboard code without rebuilding the entire image.

2. **Large image.** The single-stage build leaves the full Go toolchain (>500 MB) and build caches in the final image even though they are only needed at compile time. The resulting image is much larger than necessary for runtime.

3. **Inflexible dashboard port.** The dashboard listens on `:8080` and docker-compose.yml hardcodes the port mapping `8080:8080`. If port 8080 is already in use on the host, the user must manually edit docker-compose.yml. The Go binary accepts an `-addr` flag, but there is no way to configure the port via environment variable without rebuilding or overriding the command.

## What Changes

- Rewrite the Dockerfile as a multi-stage build: a `builder` stage compiles the Go binary, and a slim `runtime` stage copies only the binary and necessary runtime dependencies
- Add a `docker-compose.override.yml` file for development that volume-mounts the `dashboard/` source directory into the container, compiles on startup, and enables hot-reload iteration without image rebuilds
- Read a `DASHBOARD_PORT` environment variable in `dashboard/main.go` with fallback to the existing `-addr` flag default, and use `${DASHBOARD_PORT:-8080}` in docker-compose.yml for the port mapping

## Capabilities

### New Capabilities
- `dev-workflow`: Docker Compose override file for local development with source volume mount, in-container compilation, and fast iteration cycle without image rebuilds

### Modified Capabilities

## Impact

- **Dockerfile**: Rewritten as multi-stage build. Builder stage (`golang:1.26.1-bookworm` or equivalent) compiles the dashboard binary. Runtime stage (`debian:bookworm-slim`) copies only the binary and installs runtime packages. Net image size reduction.
- **docker-compose.yml**: Port mapping changes from `"8080:8080"` to `"${DASHBOARD_PORT:-8080}:${DASHBOARD_PORT:-8080}"`. `DASHBOARD_PORT` added to environment section.
- **docker-compose.override.yml**: New file. Mounts `./dashboard/` into the container, overrides command to compile and run from source. Used for local development only.
- **dashboard/main.go**: Reads `DASHBOARD_PORT` env var. If set, uses `:<value>` as listen address. Falls back to the `-addr` flag (default `:8080`) if the env var is unset.
- **.env.example**: Add `DASHBOARD_PORT=8080` entry.
- **Makefile**: Add `dev` target that uses the override file for development workflow.
- **.gitignore**: No changes needed -- `docker-compose.override.yml` is a standard Docker Compose convention and should be committed as part of the project (it defines the dev workflow, not user-specific config).

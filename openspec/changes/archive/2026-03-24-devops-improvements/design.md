## Context

The Dockerfile currently uses a single-stage build: it installs the Go toolchain, copies the dashboard source, compiles the binary, and deletes the source -- but the Go toolchain remains in the final image. The docker-compose.yml hardcodes port `8080:8080` and the Go binary accepts an `-addr` flag but not an environment variable. There is no development workflow for iterating on dashboard code without full image rebuilds.

## Goals / Non-Goals

**Goals:**
- Reduce final Docker image size by excluding build-time dependencies
- Enable a fast development cycle for dashboard code changes
- Make the dashboard port configurable without editing compose files

**Non-Goals:**
- Live/automatic hot reload (file watcher like `air`) -- a container restart is acceptable
- Changing the dashboard's functionality or API
- Modifying the non-dashboard parts of the Dockerfile (Claude Code, plugins, etc.)
- Multi-architecture builds

## Decisions

### D1: Multi-stage Dockerfile with builder and runtime stages
**Choice:** Split the Dockerfile into two stages. The `builder` stage starts from `debian:bookworm-slim`, installs Go, copies `dashboard/` source, and compiles the binary. The `runtime` stage starts fresh from `debian:bookworm-slim`, installs runtime packages (bash, git, curl, socat, tmux, bubblewrap, qrencode, npm, ca-certificates), creates the user, installs Claude Code / plugins, and copies the compiled binary from the builder stage.

`gcc` and `libc6-dev` are build-time only (needed for CGO during Go compilation) and belong in the builder stage, not the runtime stage. The dashboard binary is statically linked and does not need them at runtime. `tmux` is critical — the entire session architecture depends on it.

**Rationale:** Multi-stage builds are the standard Docker pattern for compiled languages. The Go toolchain (~600 MB) is excluded from the final image. The runtime stage only contains what is needed to run.

**Alternatives considered:**
- Keep single stage, uninstall Go after build: Removing Go after use does not reclaim space in earlier layers (Docker layer caching). A `--squash` build would help but is experimental and not universally supported.
- Separate builder image with `COPY --from`: This is exactly what we are doing -- multi-stage is the clean way to express it.

### D2: docker-compose.dev.yml for dev workflow
**Choice:** Create a `docker-compose.dev.yml` (not `docker-compose.override.yml`) that volume-mounts `./dashboard/` into the container and overrides the command to compile and run from source. The dev file uses the builder stage image (which has Go) as its base via `target: builder` in the build section, so Go is available for in-container compilation. A Makefile `dev` target invokes `docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build`.

**Rationale:** Using a named dev file instead of `docker-compose.override.yml` avoids a production hazard: override files are auto-merged by `docker compose up`, meaning `make up` would silently use the dev workflow if the override exists. With a separate file, `make up` always uses the production image and `make dev` explicitly opts into the dev workflow.

The dev file targets the builder stage (which includes Go) so compilation works inside the container without duplicating Go installation logic or polluting the runtime image.

**Alternatives considered:**
- `docker-compose.override.yml` (auto-merge): Silently changes `make up` behavior when present. Contradicts production use. Rejected.
- Install Go at dev container startup: Slow (downloads Go on every restart). Rejected.
- Keep Go in the runtime stage: Defeats the purpose of the multi-stage build. Rejected.

### D3: DASHBOARD_PORT env var with fallback to -addr flag
**Choice:** In `main.go`, check for `DASHBOARD_PORT` environment variable before applying the flag default. The precedence order is: `-addr` flag (if explicitly provided) > `DASHBOARD_PORT` env var > default `:8080`. In `docker-compose.yml`, use `${DASHBOARD_PORT:-8080}` for both the host and container port in the mapping, and pass `DASHBOARD_PORT` through to the container environment.

**Implementation detail:** Use `flag.Visit()` after `flag.Parse()` to check whether `-addr` was explicitly provided. If it was, use the flag value. Otherwise, check `os.Getenv("DASHBOARD_PORT")` -- if set, validate it is a numeric port (strip any leading colon, reject non-numeric values, log a warning and fall through to default on invalid input), then use `":" + port`. Otherwise, fall through to the flag default (`:8080`).

**Rationale:** Environment variables are the standard configuration mechanism for Docker containers. The `-addr` flag is already in use, so we preserve backward compatibility by letting it override the env var when explicitly provided.

**Alternatives considered:**
- Only env var, remove flag: Breaking change for anyone using `-addr` directly.
- Only flag, no env var: Flags are awkward in docker-compose (requires overriding `command`).
- Viper or similar config library: Overkill for a single setting.

## Risks / Trade-offs

- **[Build cache invalidation]** The multi-stage build may be slower on first build because the builder stage cannot reuse layers from the previous single-stage Dockerfile. Subsequent builds benefit from Docker layer caching as before.

- **[Dev file requires explicit flag]** Unlike an override file, `docker-compose.dev.yml` must be explicitly invoked with `-f`. Mitigation: the `make dev` target handles this; document it clearly.

- **[Port mismatch]** If `DASHBOARD_PORT` is set in `.env` but the user also hardcodes a port in an `-addr` flag override, the docker-compose port mapping and the actual listen port will diverge. Mitigation: document the precedence clearly; recommend using only the env var for Docker deployments. The compose file command does not include `-addr`.

- **[DASHBOARD_PORT validation]** If a user sets `DASHBOARD_PORT=:8080` (with colon) instead of `DASHBOARD_PORT=8080`, the listen address would be `::8080` (wrong). Mitigation: strip leading colon in main.go, validate numeric, log warning on invalid input.

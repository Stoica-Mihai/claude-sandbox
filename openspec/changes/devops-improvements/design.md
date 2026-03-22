## Context

The Dockerfile currently uses a single-stage build: it installs the Go toolchain, copies the dashboard source, compiles the binary, and deletes the source -- but the Go toolchain and build caches remain in the final image. The docker-compose.yml hardcodes port `8080:8080` and the Go binary accepts an `-addr` flag but not an environment variable. There is no development workflow for iterating on dashboard code without full image rebuilds.

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
**Choice:** Split the Dockerfile into two stages. The `builder` stage starts from the same base, installs Go, and compiles the dashboard binary. The `runtime` stage starts fresh from `debian:bookworm-slim`, installs only runtime packages (bash, git, curl, socat, bubblewrap, qrencode, npm, ca-certificates), creates the user, installs Claude Code / plugins, and copies the compiled binary from the builder stage.

**Rationale:** Multi-stage builds are the standard Docker pattern for compiled languages. The Go toolchain and build caches (easily 500+ MB) are excluded from the final image. The runtime stage only contains what is needed to run. The existing single-stage build already deletes the source after compilation, but cannot reclaim the Go toolchain layer.

**Alternatives considered:**
- Keep single stage, uninstall Go after build: Removing Go after use does not reclaim space in earlier layers (Docker layer caching). A `--squash` build would help but is experimental and not universally supported.
- Separate builder image with `COPY --from`: This is exactly what we are doing -- multi-stage is the clean way to express it.

### D2: docker-compose.override.yml for dev workflow
**Choice:** Create a `docker-compose.override.yml` that volume-mounts `./dashboard/` into the container at `/home/claude/dashboard-src/`, and overrides the command to compile and run the binary from the mounted source. Docker Compose automatically merges `docker-compose.override.yml` when present.

**Rationale:** This is the standard Docker Compose pattern for development overrides. It does not require any flags -- `docker compose up` automatically picks up the override. Production use simply removes or renames the file. The source mount means edits on the host are immediately available in the container; restarting the container recompiles.

**Alternatives considered:**
- `docker compose -f docker-compose.yml -f docker-compose.dev.yml`: More explicit but requires remembering the `-f` flags. The override convention is simpler.
- `air` or `watchexec` for live reload: Adds a dependency and complexity. The Go compile is fast (~2-3s); a manual restart is acceptable for this project's scale.
- Makefile `dev` target: Still useful as a convenience wrapper, but the override file is the core mechanism.

### D3: DASHBOARD_PORT env var with fallback to -addr flag
**Choice:** In `main.go`, check for `DASHBOARD_PORT` environment variable before applying the flag default. The precedence order is: `-addr` flag (if explicitly provided) > `DASHBOARD_PORT` env var > default `:8080`. In `docker-compose.yml`, use `${DASHBOARD_PORT:-8080}` for both the host and container port in the mapping, and pass `DASHBOARD_PORT` through to the container environment.

**Rationale:** Environment variables are the standard configuration mechanism for Docker containers. The `-addr` flag is already in use, so we preserve backward compatibility by letting it override the env var when explicitly provided. The `${DASHBOARD_PORT:-8080}` syntax in docker-compose.yml is idiomatic and requires no changes to `.env` for the default case.

**Implementation detail:** Go's `flag` package does not distinguish "flag was explicitly set" from "flag has its default value" without extra work. Use `flag.Visit()` after `flag.Parse()` to check whether `-addr` was explicitly provided. If it was, use the flag value. Otherwise, check `os.Getenv("DASHBOARD_PORT")` -- if set, use `":" + value`. Otherwise, fall through to the flag default (`:8080`).

**Alternatives considered:**
- Only env var, remove flag: Breaking change for anyone using `-addr` directly.
- Only flag, no env var: Flags are awkward in docker-compose (requires overriding `command`).
- Viper or similar config library: Overkill for a single setting.

## Risks / Trade-offs

- **[Build cache invalidation]** The multi-stage build may be slower on first build because the builder stage cannot reuse layers from the previous single-stage Dockerfile. Subsequent builds benefit from Docker layer caching as before.

- **[Override file confusion]** Developers unfamiliar with the `docker-compose.override.yml` convention may not realize it is being merged automatically. Mitigation: document in the Makefile and README, and add a comment at the top of the override file explaining its purpose.

- **[Port mismatch]** If `DASHBOARD_PORT` is set in `.env` but the user also hardcodes a port in an `-addr` flag override, the docker-compose port mapping and the actual listen port will diverge. Mitigation: document the precedence clearly; recommend using only the env var for Docker deployments.

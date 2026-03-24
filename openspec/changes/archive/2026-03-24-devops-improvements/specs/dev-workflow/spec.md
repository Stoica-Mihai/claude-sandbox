## ADDED Requirements

### Requirement: Multi-stage Dockerfile
The Dockerfile SHALL use a multi-stage build with at least two stages: a builder stage that compiles the Go dashboard binary (with Go toolchain, gcc, libc6-dev), and a runtime stage that contains only the compiled binary and runtime dependencies (bash, git, curl, socat, tmux, bubblewrap, qrencode, npm, ca-certificates). The Go toolchain, gcc, libc6-dev, source code, and Go module cache SHALL NOT be present in the final runtime image. The runtime stage SHALL preserve `USER claude` and `WORKDIR /workspace`.

#### Scenario: Builder stage compiles the dashboard
- **WHEN** the Docker image is built
- **THEN** the builder stage SHALL compile the Go dashboard binary using the Go toolchain
- **AND** the runtime stage SHALL copy only the compiled binary from the builder stage

#### Scenario: Final image excludes build tools
- **WHEN** the final Docker image is inspected
- **THEN** the Go toolchain (`/usr/local/go/`), gcc, libc6-dev, dashboard source code, and Go module cache SHALL NOT be present in the image filesystem
- **AND** tmux SHALL be present and functional in the runtime image

#### Scenario: Existing functionality preserved
- **WHEN** the container starts from the new multi-stage image
- **THEN** all existing functionality (Claude Code, plugins, dashboard server, tmux sessions, bash aliases, non-root user) SHALL work identically to the single-stage build

### Requirement: Dev workflow via explicit compose file
A `docker-compose.dev.yml` file (NOT `docker-compose.override.yml`) SHALL provide a development workflow that volume-mounts the dashboard source directory into the container and compiles from source on startup. The dev file SHALL target the builder stage of the multi-stage Dockerfile so the Go toolchain is available for in-container compilation. A Makefile `dev` target SHALL invoke the dev compose file explicitly.

#### Scenario: Dev mode with source mount
- **WHEN** the developer runs `make dev`
- **THEN** docker compose SHALL be invoked with `-f docker-compose.yml -f docker-compose.dev.yml`
- **AND** the `./dashboard/` directory from the host SHALL be mounted into the container
- **AND** the container SHALL have the Go toolchain available (from the builder stage)
- **AND** the container SHALL compile the Go binary from the mounted source on startup
- **AND** the dashboard server SHALL start from the freshly compiled binary

#### Scenario: Code change iteration
- **WHEN** the developer modifies a file in `./dashboard/` on the host
- **THEN** restarting the container via `make dev` SHALL pick up the changes without a full image rebuild

#### Scenario: Production is not affected by dev file
- **WHEN** the developer runs `make up` (production mode)
- **THEN** docker compose SHALL NOT merge `docker-compose.dev.yml` automatically
- **AND** the container SHALL use the pre-compiled binary baked into the production image

### Requirement: Configurable dashboard port via environment variable
The dashboard server SHALL read a `DASHBOARD_PORT` environment variable to determine its listen port. The environment variable SHALL take precedence over the default, but the `-addr` flag SHALL take precedence over both (for backward compatibility). Invalid values (non-numeric, empty after stripping leading colon) SHALL be logged as a warning and the default SHALL be used.

#### Scenario: DASHBOARD_PORT set
- **WHEN** the `DASHBOARD_PORT` environment variable is set (e.g., `DASHBOARD_PORT=9090`)
- **THEN** the dashboard server SHALL listen on `:<DASHBOARD_PORT>` (e.g., `:9090`)

#### Scenario: DASHBOARD_PORT unset, flag not provided
- **WHEN** the `DASHBOARD_PORT` environment variable is not set and the `-addr` flag is not provided
- **THEN** the dashboard server SHALL listen on `:8080` (the existing default)

#### Scenario: Flag overrides env var
- **WHEN** both `DASHBOARD_PORT=9090` and `-addr :3000` are provided
- **THEN** the dashboard server SHALL listen on `:3000` (flag takes precedence)

#### Scenario: Invalid DASHBOARD_PORT
- **WHEN** `DASHBOARD_PORT` is set to a non-numeric value or includes a leading colon (e.g., `:8080`)
- **THEN** the server SHALL strip the leading colon, attempt to parse the remainder as numeric, log a warning if invalid, and fall through to the default `:8080`

#### Scenario: Docker Compose port mapping
- **WHEN** `DASHBOARD_PORT` is set in the `.env` file or host environment
- **THEN** `docker-compose.yml` SHALL use that value for both the host and container port mapping (e.g., `9090:9090`)
- **AND** when `DASHBOARD_PORT` is not set, the mapping SHALL default to `8080:8080`

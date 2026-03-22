## ADDED Requirements

### Requirement: Multi-stage Dockerfile
The Dockerfile SHALL use a multi-stage build with at least two stages: a builder stage that compiles the Go dashboard binary, and a runtime stage that contains only the compiled binary and runtime dependencies. The Go toolchain, source code, and build caches SHALL NOT be present in the final image.

#### Scenario: Builder stage compiles the dashboard
- **WHEN** the Docker image is built
- **THEN** the builder stage SHALL compile the Go dashboard binary using the Go toolchain
- **AND** the runtime stage SHALL copy only the compiled binary from the builder stage

#### Scenario: Final image excludes build tools
- **WHEN** the final Docker image is inspected
- **THEN** the Go toolchain (`/usr/local/go/`), dashboard source code, and Go module cache SHALL NOT be present in the image filesystem

#### Scenario: Existing functionality preserved
- **WHEN** the container starts from the new multi-stage image
- **THEN** all existing functionality (Claude Code, plugins, dashboard server, bash aliases, non-root user) SHALL work identically to the single-stage build

### Requirement: Docker Compose override for development
A `docker-compose.override.yml` file SHALL provide a development workflow that volume-mounts the dashboard source directory into the container, enabling code changes to take effect without rebuilding the Docker image.

#### Scenario: Dev mode with source mount
- **WHEN** the developer runs `docker compose up` with the override file active
- **THEN** the `./dashboard/` directory from the host SHALL be mounted into the container
- **AND** the container SHALL compile the Go binary from the mounted source on startup
- **AND** the dashboard server SHALL start from the freshly compiled binary

#### Scenario: Code change iteration
- **WHEN** the developer modifies a file in `./dashboard/` on the host
- **THEN** restarting the container (or the dev command) SHALL pick up the changes without a full `docker compose build`

#### Scenario: Override does not affect production
- **WHEN** the override file is removed or not present
- **THEN** `docker compose up` SHALL use the pre-compiled binary baked into the image, as it does today

### Requirement: Configurable dashboard port via environment variable
The dashboard server SHALL read a `DASHBOARD_PORT` environment variable to determine its listen port. The environment variable SHALL take precedence over the default, but the `-addr` flag SHALL take precedence over both (for backward compatibility).

#### Scenario: DASHBOARD_PORT set
- **WHEN** the `DASHBOARD_PORT` environment variable is set (e.g., `DASHBOARD_PORT=9090`)
- **THEN** the dashboard server SHALL listen on `:<DASHBOARD_PORT>` (e.g., `:9090`)

#### Scenario: DASHBOARD_PORT unset, flag not provided
- **WHEN** the `DASHBOARD_PORT` environment variable is not set and the `-addr` flag is not provided
- **THEN** the dashboard server SHALL listen on `:8080` (the existing default)

#### Scenario: Flag overrides env var
- **WHEN** both `DASHBOARD_PORT=9090` and `-addr :3000` are provided
- **THEN** the dashboard server SHALL listen on `:3000` (flag takes precedence)

#### Scenario: Docker Compose port mapping
- **WHEN** `DASHBOARD_PORT` is set in the `.env` file or host environment
- **THEN** `docker-compose.yml` SHALL use that value for both the host and container port mapping (e.g., `9090:9090`)
- **AND** when `DASHBOARD_PORT` is not set, the mapping SHALL default to `8080:8080`

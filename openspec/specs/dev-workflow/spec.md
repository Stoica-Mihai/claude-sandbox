# dev-workflow Specification

## Purpose

Defines requirements for the development workflow infrastructure: two multi-stage Docker builds (one per service) that minimize runtime image size, a `docker compose watch` based dev loop that rebuilds the affected service on source changes, and the supporting Makefile targets for building, running, and operating the containers.

## Requirements

### Requirement: Multi-stage Dockerfile per service
The repository SHALL provide two separate multi-stage Dockerfiles, `Dockerfile.backend` and `Dockerfile.frontend`, one per service. Each Dockerfile MUST have a builder stage that compiles its Go service binary (with the Go toolchain, gcc, and libc6-dev) and a slim runtime stage that contains only the compiled binary plus that service's runtime dependencies. The Go toolchain, gcc, libc6-dev, source code, and Go module cache MUST NOT be present in either final runtime image, except that the backend runtime image SHALL retain Go so that `go test -race` can run in-container. The backend runtime SHALL include dtach, bash, git, curl, bubblewrap, qrencode, npm, and ca-certificates; the frontend runtime SHALL be minimal and SHALL NOT include dtach, socat, or claude. Neither image SHALL contain tmux or socat. Both runtime stages MUST run as non-root `USER claude` with `WORKDIR /workspace`.

#### Scenario: Builder stages compile the service binaries
- **WHEN** the backend and frontend images are built
- **THEN** the builder stage of each Dockerfile SHALL compile its Go service binary using the Go toolchain, gcc, and libc6-dev
- **AND** each runtime stage SHALL copy only the compiled binary from its builder stage

#### Scenario: Final images exclude build tooling
- **WHEN** the final frontend image is inspected
- **THEN** the Go toolchain, gcc, libc6-dev, service source code, and Go module cache SHALL NOT be present in its filesystem
- **WHEN** the final backend image is inspected
- **THEN** gcc, libc6-dev, service source code, and Go module cache SHALL NOT be present, but Go SHALL remain so `go test -race` can run in-container

#### Scenario: Service-specific runtime dependencies
- **WHEN** the backend runtime image is inspected
- **THEN** dtach, bash, git, curl, bubblewrap, qrencode, npm, and ca-certificates SHALL be present
- **AND** tmux and socat SHALL NOT be present
- **WHEN** the frontend runtime image is inspected
- **THEN** it SHALL be minimal and SHALL NOT contain dtach, socat, claude, tmux, or the Go toolchain

#### Scenario: Non-root user preserved
- **WHEN** a container starts from either runtime image
- **THEN** the process SHALL run as the non-root user `claude`
- **AND** the working directory SHALL be `/workspace`

### Requirement: Dev workflow via docker compose watch
The development workflow SHALL be driven by `docker compose watch`, exposed through the Makefile `watch` target. Each service in `docker-compose.yml` MUST declare a `develop.watch` entry with a `rebuild` action keyed on its own source directory (`./backend/` for backend, `./frontend/` for frontend) so that editing a source file rebuilds and restarts only the affected service. There SHALL NOT be a `docker-compose.dev.yml` file or a `make dev` target.

#### Scenario: Watch rebuilds on backend source change
- **WHEN** the developer runs `make watch` and then modifies a file under `./backend/`
- **THEN** `docker compose watch` SHALL trigger the `rebuild` action for the backend service
- **AND** the backend container SHALL be rebuilt and restarted from the updated source

#### Scenario: Watch rebuilds on frontend source change
- **WHEN** the developer runs `make watch` and then modifies a file under `./frontend/`
- **THEN** `docker compose watch` SHALL trigger the `rebuild` action for the frontend service
- **AND** the frontend container SHALL be rebuilt and restarted from the updated source

#### Scenario: No dev compose file or dev target exists
- **WHEN** the repository is inspected
- **THEN** there SHALL be no `docker-compose.dev.yml` file
- **AND** there SHALL be no `make dev` target

### Requirement: Makefile operational targets
The Makefile SHALL provide targets for building, running, and operating the two-service stack. `make up` MUST build and start the backend and frontend services; `make down` MUST stop them; `make build` MUST build the images; `make rebuild` MUST build the images without cache; `make watch` MUST run `docker compose watch`; `make shell` MUST open a bash shell in the backend container; and `make restart-backend` and `make restart-frontend` MUST rebuild and restart only their respective service.

#### Scenario: Bring the stack up and down
- **WHEN** the developer runs `make up`
- **THEN** the backend and frontend services SHALL be built and started
- **WHEN** the developer runs `make down`
- **THEN** both services SHALL be stopped

#### Scenario: Build targets
- **WHEN** the developer runs `make build`
- **THEN** the service images SHALL be built
- **WHEN** the developer runs `make rebuild`
- **THEN** the service images SHALL be built without using the cache

#### Scenario: Per-service restart and shell access
- **WHEN** the developer runs `make restart-backend` or `make restart-frontend`
- **THEN** only the named service SHALL be rebuilt and restarted
- **WHEN** the developer runs `make shell`
- **THEN** a bash shell SHALL open inside the backend container

### Requirement: Configurable dashboard port via environment variable
The frontend dashboard server SHALL read a `DASHBOARD_PORT` environment variable to determine its listen port, defaulting to `8080` when unset. The published host port mapping in `docker-compose.yml` SHALL use `DASHBOARD_PORT` for both the host and container side, defaulting to `8080:8080` when unset. The backend SHALL listen on `BACKEND_PORT` (default `8081`) on the internal compose network with no published host ports.

#### Scenario: DASHBOARD_PORT set
- **WHEN** `DASHBOARD_PORT` is set (e.g., `DASHBOARD_PORT=9090`)
- **THEN** the dashboard server SHALL listen on `:9090`
- **AND** `docker-compose.yml` SHALL publish the mapping `9090:9090`

#### Scenario: DASHBOARD_PORT unset
- **WHEN** `DASHBOARD_PORT` is not set
- **THEN** the dashboard server SHALL listen on `:8080`
- **AND** `docker-compose.yml` SHALL publish the mapping `8080:8080`

#### Scenario: Backend port is internal only
- **WHEN** the stack is running
- **THEN** the backend SHALL listen on `BACKEND_PORT` (default `8081`) reachable only on the internal compose network
- **AND** the backend SHALL NOT publish any host port

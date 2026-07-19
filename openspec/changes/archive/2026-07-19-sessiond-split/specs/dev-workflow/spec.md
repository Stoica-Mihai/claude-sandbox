# dev-workflow Delta

## MODIFIED Requirements

### Requirement: Multi-stage Dockerfile per service
The repository SHALL provide a multi-stage Dockerfile per first-party service: `Dockerfile.sessions` (sessiond), `Dockerfile.backend`, and `Dockerfile.frontend` (the holesail sidecar keeps its own `Dockerfile.holesail`). Each MUST have a builder stage that compiles its Go binary and a runtime stage containing only the binary plus that service's runtime dependencies; source code, gcc, libc6-dev, and the Go module cache MUST NOT be present in any final runtime image.

- The **sessions** runtime SHALL carry the heavy session environment — claude, bash, git, curl, bubblewrap, qrencode, npm, ca-certificates, and the Go toolchain (so `go test -race` can run in-container) — and SHALL NOT contain dtach, tmux, or socat. It runs sessiond as the non-root shared-UID user with `WORKDIR /workspace`.
- The **backend** runtime SHALL be slim: the backend binary, curl (healthcheck), and ca-certificates only — no claude, no dtach, no npm, no Go toolchain. It runs as the same shared-UID user so the socket volume is writable by both.
- The **frontend** runtime SHALL remain minimal and unchanged.

#### Scenario: Builder stages compile the service binaries
- **WHEN** the sessions, backend, and frontend images are built
- **THEN** each builder stage SHALL compile its Go binary and each runtime stage SHALL copy only the compiled binary from its builder

#### Scenario: dtach absent everywhere
- **WHEN** all runtime images are inspected
- **THEN** none SHALL contain dtach, tmux, or socat

#### Scenario: Backend image is slim
- **WHEN** the final backend image is inspected
- **THEN** claude, npm, the Go toolchain, gcc, libc6-dev, and source code SHALL NOT be present

#### Scenario: Sessions image carries the session environment
- **WHEN** the final sessions image is inspected
- **THEN** claude, git, bubblewrap, npm, and Go SHALL be present, and the container SHALL run as the non-root shared-UID user with `WORKDIR /workspace`

### Requirement: Dev workflow via docker compose watch
The development workflow SHALL be driven by `docker compose watch`, exposed through the Makefile `watch` target. Each service in `docker-compose.yml` MUST declare a `develop.watch` entry with a `rebuild` action keyed on its source directories: `./sessiond/` for the sessions service; `./backend/`, `./shared/`, and `./sessiond/` (protocol dependency) for the backend; `./frontend/` and `./shared/` for the frontend. Editing backend or frontend source SHALL NOT rebuild the sessions service, so running claude sessions survive the dev loop. There SHALL NOT be a `docker-compose.dev.yml` file or a `make dev` target.

#### Scenario: Watch rebuilds on backend source change without killing sessions
- **WHEN** the developer runs `make watch` and then modifies a file under `./backend/`
- **THEN** only the backend service SHALL be rebuilt and restarted, and running claude sessions SHALL keep running with viewers reconnecting automatically

#### Scenario: Watch rebuilds on sessiond source change
- **WHEN** the developer modifies a file under `./sessiond/`
- **THEN** the sessions service SHALL be rebuilt (ending running sessions) and the backend SHALL be rebuilt (protocol dependency)

#### Scenario: Watch rebuilds on frontend source change
- **WHEN** the developer modifies a file under `./frontend/`
- **THEN** only the frontend service SHALL be rebuilt and restarted

### Requirement: Makefile operational targets
The Makefile SHALL provide targets for building, running, and operating the stack. `make up` MUST build and start all services; `make down` MUST stop them; `make build` / `make rebuild` MUST build the images (without cache for rebuild); `make watch` MUST run `docker compose watch`; `make shell` MUST open a bash shell in the **sessions** container (where claude and the workspace tooling live); and `make restart-sessions`, `make restart-backend`, `make restart-frontend`, and `make restart-holesail` MUST rebuild and restart only their respective service.

#### Scenario: Bring the stack up and down
- **WHEN** the developer runs `make up`
- **THEN** the sessions, backend, frontend, and holesail services SHALL be built and started
- **WHEN** the developer runs `make down`
- **THEN** all services SHALL be stopped

#### Scenario: Per-service restart and shell access
- **WHEN** the developer runs `make restart-sessions`, `make restart-backend`, or `make restart-frontend`
- **THEN** only the named service SHALL be rebuilt and restarted
- **WHEN** the developer runs `make shell`
- **THEN** a bash shell SHALL open inside the sessions container

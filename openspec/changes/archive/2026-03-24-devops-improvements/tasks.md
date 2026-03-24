## 1. Multi-stage Dockerfile

- [x] 1.1 Add a `builder` stage to the Dockerfile: start from `debian:bookworm-slim`, install Go, install `gcc` and `libc6-dev` (for CGO), copy `dashboard/` source, run `go build -o /dashboard-server .`
- [x] 1.2 Restructure the runtime stage: start fresh from `debian:bookworm-slim`, install runtime packages (bash, git, curl, socat, tmux, bubblewrap, qrencode, npm, ca-certificates), create user, install Claude Code, plugins, UV, OpenSpec — everything except Go, gcc, libc6-dev
- [x] 1.3 Copy the compiled binary from the builder stage: `COPY --from=builder /dashboard-server /home/claude/dashboard-server`
- [x] 1.4 Ensure `USER claude` is set before Claude Code install steps and `WORKDIR /workspace` is preserved at the end of the runtime stage
- [x] 1.5 Verify the built image does not contain `/usr/local/go/` and that `tmux` is present and functional

## 2. Configurable dashboard port

- [x] 2.1 Update `dashboard/main.go`: after `flag.Parse()`, use `flag.Visit()` to check if `-addr` was explicitly provided. If not, check `os.Getenv("DASHBOARD_PORT")` — strip any leading colon, validate it is numeric (log warning and fall through to default on invalid input), and set addr to `":" + port`
- [x] 2.2 Update `docker-compose.yml` port mapping from `"8080:8080"` to `"${DASHBOARD_PORT:-8080}:${DASHBOARD_PORT:-8080}"`
- [x] 2.3 Add `DASHBOARD_PORT` to the `environment` section of `docker-compose.yml`: `DASHBOARD_PORT=${DASHBOARD_PORT:-8080}`
- [x] 2.4 Add `DASHBOARD_PORT=8080` with a comment to `.env.example`
- [x] 2.5 Verify `generate-env.sh` copies `.env.example` as-is (so `DASHBOARD_PORT` propagates) — if it uses sed to substitute specific variables, update it to handle the new variable

## 3. Dev workflow via docker-compose.dev.yml

- [x] 3.1 Create `docker-compose.dev.yml` that extends the `claude-env` service with: `build.target: builder` (uses the builder stage which has Go), volume mount `./dashboard/:/home/claude/dashboard-src/:ro`, and command override `sh -c "cd /home/claude/dashboard-src && go build -o /tmp/dashboard-server . && exec /tmp/dashboard-server"`
- [x] 3.2 Add a comment at the top of `docker-compose.dev.yml` explaining its purpose and that it must be invoked via `make dev`
- [x] 3.3 Add Makefile `dev` target: `docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build`
- [x] 3.4 Ensure `docker-compose.dev.yml` is NOT named `docker-compose.override.yml` to avoid auto-merge on `make up`

## 4. Verification

- [x] 4.1 Build the production image with `make up` and verify the dashboard starts on default port 8080
- [x] 4.2 Set `DASHBOARD_PORT=9090` in `.env`, run `make up`, verify the dashboard is accessible on port 9090
- [x] 4.3 Verify dev mode: run `make dev`, edit a Go file in `dashboard/`, restart the container with `make dev`, confirm the change is picked up without a full image rebuild
- [x] 4.4 Verify `make up` does NOT use dev mode (no Go compilation, uses pre-built binary)
- [x] 4.5 Verify `make shell`, `make claude`, and `make down` still work
- [x] 4.6 Compare final production image size before and after the multi-stage refactor

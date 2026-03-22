## 1. Multi-stage Dockerfile

- [ ] 1.1 Add a `builder` stage to the Dockerfile: start from `debian:bookworm-slim`, install Go 1.26.1, copy `dashboard/` source, run `go build -o /dashboard-server .`
- [ ] 1.2 Restructure the runtime stage: start fresh from `debian:bookworm-slim`, install runtime packages (bash, git, curl, socat, bubblewrap, qrencode, npm, ca-certificates), create user, install Claude Code, plugins, UV, OpenSpec -- everything except Go
- [ ] 1.3 Copy the compiled binary from the builder stage into the runtime stage with `COPY --from=builder /dashboard-server /home/claude/dashboard-server`
- [ ] 1.4 Remove the Go toolchain installation from the runtime stage (the `curl | tar` for Go and the `PATH` addition for `/usr/local/go/bin`)
- [ ] 1.5 Verify the built image does not contain `/usr/local/go/` or `/home/claude/dashboard-src/`

## 2. Configurable dashboard port

- [ ] 2.1 Update `dashboard/main.go`: after `flag.Parse()`, use `flag.Visit()` to check if `-addr` was explicitly provided. If not, check `os.Getenv("DASHBOARD_PORT")` and set addr to `":" + port` if present
- [ ] 2.2 Update `docker-compose.yml` port mapping from `"8080:8080"` to `"${DASHBOARD_PORT:-8080}:${DASHBOARD_PORT:-8080}"`
- [ ] 2.3 Add `DASHBOARD_PORT` to the `environment` section of `docker-compose.yml` so it is passed into the container: `- DASHBOARD_PORT=${DASHBOARD_PORT:-8080}`
- [ ] 2.4 Add `DASHBOARD_PORT=8080` to `.env.example` with a comment

## 3. Dev workflow via docker-compose.override.yml

- [ ] 3.1 Create `docker-compose.override.yml` with a volume mount of `./dashboard/:/home/claude/dashboard-src/` and a command override that compiles from source and runs the binary (e.g., `sh -c "cd /home/claude/dashboard-src && go build -o /tmp/dashboard-server . && exec /tmp/dashboard-server"`)
- [ ] 3.2 Ensure the override file mounts the Go toolchain or that the runtime stage retains Go for dev mode -- alternatively, use the builder stage image as the dev target. Decide based on D2: since the override merges with the main compose, and the runtime image no longer has Go, the override must either install Go at startup or use a different image. Simplest approach: add a `dev` profile/service that extends `claude-env` and uses the builder stage image, or keep Go in the runtime stage gated behind dev usage
- [ ] 3.3 Add a Makefile `dev` target that runs `docker compose up -d --build` with the override (default behavior when override file exists)
- [ ] 3.4 Add a comment at the top of `docker-compose.override.yml` explaining its purpose and that Docker Compose merges it automatically

## 4. Verification

- [ ] 4.1 Build the image with `docker compose build` and verify it starts correctly on the default port 8080
- [ ] 4.2 Set `DASHBOARD_PORT=9090` in `.env`, rebuild, and verify the dashboard is accessible on port 9090
- [ ] 4.3 Verify dev mode: with the override file active, edit a file in `dashboard/`, restart the container, and confirm the change is picked up without a full image rebuild
- [ ] 4.4 Verify `make up`, `make shell`, `make claude`, and `make down` still work as before
- [ ] 4.5 Compare final image size before and after the multi-stage refactor

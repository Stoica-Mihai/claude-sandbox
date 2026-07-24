# service-logging Specification

## Purpose
TBD - created by archiving change add-logd-logging-phase1. Update Purpose after archive.
## Requirements
### Requirement: Durable structured log persistence

When `LOG_DIR` is set, `InitLogging(service)` SHALL persist every `slog` record as one JSON line, appended (`O_APPEND`) to `<LOG_DIR>/<service>.log`, so a record is durable on the local filesystem the instant it is written — no network in the write path, no bounded in-memory buffer that could drop under load.

#### Scenario: slog record persisted as a JSON line
- **WHEN** a service initialized with `LOG_DIR=/logs` and name `backend` emits a log record
- **THEN** a single JSON line containing the record's `time`, `level`, `msg`, a `service` attr, and any custom attrs is appended to `/logs/backend.log`

### Requirement: Crash output is captured, not bypassed

The producer SHALL redirect fd 2 onto the log file (`dup2`) so that a `panic`, `log.Fatal`, or any direct `os.Stderr` write — which bypass the `slog` handler — is still written to the log file before the process exits.

#### Scenario: raw stderr write lands in the file
- **WHEN** code writes directly to `os.Stderr` (e.g. a panic stack) after `InitLogging` with `LOG_DIR` set
- **THEN** those bytes appear in `<LOG_DIR>/<service>.log`, not only on the console

### Requirement: Console mirror preserves `docker logs`

The producer SHALL also mirror `slog` output, in human-readable text form, to the original console stderr, so `docker logs <service>` remains usable and readable after the fd-2 redirect.

#### Scenario: slog line still visible on the console as text
- **WHEN** a service with `LOG_DIR` set emits a log record
- **THEN** a human-readable text line for that record is written to the original console stderr (not JSON), in addition to the JSON line in the file

### Requirement: Bounded log rotation

The file sink SHALL rotate `<service>.log` when it exceeds a size cap, keeping a bounded number of generations (`.1 .. .N`), so disk usage per service is bounded.

#### Scenario: oversized file rotates and stays bounded
- **WHEN** `<service>.log` grows past the configured cap
- **THEN** it is renamed to `<service>.log.1` (older generations shifting up to `.N`, the oldest discarded) and a fresh `<service>.log` is opened for subsequent writes

### Requirement: Graceful degradation on sink failure

If the log file cannot be opened or fd 2 cannot be redirected, `InitLogging` SHALL warn and continue with stderr-only logging; a logging-setup failure MUST NOT crash the service it exists to observe.

#### Scenario: unwritable log dir does not crash the service
- **WHEN** `LOG_DIR` is set to a path the process cannot open for writing
- **THEN** `InitLogging` logs a warning and returns, the service keeps running, and logs go to stderr only

### Requirement: Non-container mode leaves behavior unchanged

When `LOG_DIR` is unset, `InitLogging` SHALL install only the existing stderr text handler and create no file and perform no fd redirect, so local `go test` and non-container runs are unaffected.

#### Scenario: LOG_DIR unset means stderr-only
- **WHEN** `InitLogging(service)` runs with `LOG_DIR` unset
- **THEN** no log file is created, fd 2 is not redirected, and logging goes to stderr as before


# Spec: dtach-sessions

**Spec Path:** `specs/dtach-sessions/spec.md`
**Change Type:** MODIFIED

---

## MODIFIED Requirements

### Requirement: Spawn Claude Code sessions as detached dtach masters
The system SHALL spawn each Claude Code session as a detached `dtach` master that holds the `claude` process, created with `dtach -n <sockdir>/claude-<hex> -E -z bash -c '...'`. The system SHALL generate a UUIDv4 conversation id and launch claude with `--session-id <uuid> --dangerously-skip-permissions` so the dashboard owns the conversation id. The inner command SHALL write the claude process PID to the PID sidecar before exec, and the spawner SHALL write a metadata sidecar containing the working directory, creation time, and the conversation uuid. The working directory MUST exist and MUST be under `/workspace`. On socket-name collision the system SHALL retry with a new random name up to 3 times.

#### Scenario: Spawn records a conversation uuid
- **WHEN** a spawn request is made with `cwd=/workspace/my-project`
- **THEN** the system SHALL generate a UUIDv4, launch `claude --session-id <uuid> --dangerously-skip-permissions`, store the uuid in the session's metadata sidecar, and record it in the session index

#### Scenario: Working directory outside workspace
- **WHEN** a spawn request specifies a cwd not under `/workspace`
- **THEN** the system SHALL reject the request with an error

## ADDED Requirements

### Requirement: Persisted session index
The system SHALL maintain a dashboard-owned session index at `$CLAUDE_CONFIG_DIR/dashboard-sessions.json`, mapping each conversation uuid to its working directory, creation time, and optional custom name. The index SHALL be written atomically under a mutex. Because it lives in the persistent, host-mounted config dir, the index and custom names SHALL survive container restarts. The index SHALL be the source for the per-folder resume list and for custom display names. The system SHALL NOT read claude's transcript files or depend on claude's on-disk directory layout to build the list.

#### Scenario: Index entry written on spawn
- **WHEN** a new session is spawned in `/workspace/cmux`
- **THEN** the index SHALL gain an entry keyed by the conversation uuid with that cwd and the creation time

#### Scenario: Names persist across container restart
- **WHEN** a session has been renamed and the backend container is recreated
- **THEN** the custom name SHALL still be present in the index and SHALL display in the sidebar and resume list

#### Scenario: Index unaffected by claude storage changes
- **WHEN** claude's transcript storage format or layout changes
- **THEN** the resume list SHALL still be built from the dashboard index, and at worst show no entries — it SHALL NOT error

### Requirement: Resume a recorded conversation
The system SHALL resume a conversation by spawning a new detached dtach session whose inner command runs `claude --resume <uuid> --dangerously-skip-permissions` in the conversation's recorded working directory. Resuming SHALL reuse the existing conversation uuid and SHALL NOT create a duplicate index entry. A resumed conversation SHALL behave as a normal live session (relay, sidebar, kill).

#### Scenario: Resume an existing conversation
- **WHEN** a resume request is made for a known conversation uuid
- **THEN** the system SHALL spawn a dtach session running `claude --resume <uuid>` in that conversation's cwd and it SHALL appear as a live session

#### Scenario: Resume is delegated to claude
- **WHEN** the conversation cannot be resumed (e.g. claude has no such transcript)
- **THEN** claude SHALL report the condition in the session terminal; the dashboard SHALL NOT parse transcripts to validate resumability

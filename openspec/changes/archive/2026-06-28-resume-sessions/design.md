# Design: Resume past sessions per folder

## Context

The dashboard creates sessions; claude persists each conversation under
`$CLAUDE_CONFIG_DIR`. To resume, the dashboard must (a) know a conversation's
stable id and (b) re-launch claude on it. claude exposes `--session-id <uuid>`
(use a chosen id) and `--resume <uuid>` (reopen by id). We build on those rather
than reading claude's transcript files.

## Goals / Non-Goals

**Goals**
- See a folder's previous sessions in the New Session modal and resume one.
- Show a custom name when the user has renamed a session; otherwise time + id.
- Survive container restarts (names + list persist).
- Zero parsing of claude's transcript format; no dependence on claude's on-disk
  layout/encoding.

**Non-Goals**
- Listing conversations not created by the dashboard.
- Editing/deleting transcripts.
- Showing message counts or AI summaries (would require reading transcript content).

## Decisions

### Decision 1: Dashboard owns the conversation id via `--session-id`
On spawn the backend generates a UUIDv4 and runs:

```
dtach -n <sock> -E -z bash -c 'echo $$ > <pid>; exec claude --session-id <uuid> --dangerously-skip-permissions'
```

Owning the id removes any need to discover it by parsing claude output or scanning
claude's `projects/` dir (whose cwd-encoding is an undocumented implementation
detail). The uuid is stored in the session's metadata sidecar (`sessionMeta.SessionID`)
so a live dtach session can be mapped to its conversation.

### Decision 2: Persisted, uuid-keyed session index (list source)
A single dashboard-owned file `$CLAUDE_CONFIG_DIR/dashboard-sessions.json`:

```json
{ "<uuid>": { "cwd": "/workspace/cmux", "created": 1782600000, "name": "relay fixes" } }
```

- Lives in `$CLAUDE_CONFIG_DIR` (`~/.claude-sandbox`, host-mounted) so it persists
  across container restarts.
- Written on spawn (`{cwd, created}`), updated by rename (`name`).
- The resume list for a folder = entries whose `cwd` matches, sorted by `created`
  descending.
- Replaces the old `session-names.json` in the (non-persistent) meta dir. Custom
  names now persist as a side effect.

Access is serialized with a mutex and written atomically (temp + rename).

**Why an index instead of scanning `projects/<encoded-cwd>/*.jsonl`:** names must
be keyed by uuid regardless, so a uuid index already has to exist; making it the
list source also removes the claude-storage-layout dependency (the encoding guess)
and means a claude change can at most empty the list, never break it. Resume is
still delegated to `claude --resume <uuid>`.

### Decision 3: Resume = a new dtach session running `--resume`
`POST /api/sessions` with `resume=<uuid>` spawns a normal dtach session whose
inner command is `claude --resume <uuid> --dangerously-skip-permissions` in the
recorded cwd. It reuses the existing uuid (no new index entry). Everything else
(relay, sidebar, kill) is unchanged — a resumed conversation is just another live
session.

### Decision 4: Names + history by uuid
- Rename (`PUT /api/sessions/{terminalId}/name`): resolve the live session's uuid
  from its metadata sidecar, then set `index[uuid].name`.
- Sidebar display name (`enrichSessions`): resolve each live session's uuid →
  `index[uuid].name`.
- `GET /api/sessions/history?cwd=`: return `[{uuid, created, name}]` for that cwd.

### Decision 5: Frontend modal (matches approved mockup)
- Breadcrumb + folder rows (click navigates).
- Inside a folder: "Start a new session" row (pre-selected by default) + a
  "Previous sessions" list fetched from `/api/sessions/history`.
- Selection = background-color change only (no radio, no edge bar); single-select.
- Footer: permanent **Cancel** + one primary button that reads **Launch** (new
  selected) or **Resume** (session selected), relabeled in place; disabled at the
  workspace root.
- Submitting posts `{cwd}` (new) or `{cwd, resume:<uuid>}` (resume).

## Risks / Trade-offs
- Index/list is dashboard-scoped: sessions created outside the dashboard (none, CLI
  is disabled) or before this change won't appear. Acceptable.
- If `dashboard-sessions.json` is lost, the resume list empties but live sessions
  and new spawns are unaffected.

## Verification
- `go build`/`vet`/`test -race` in `backend/` and `frontend/`.
- Manual: spawn → uuid recorded; rename → name shows in sidebar and history;
  reopen modal in that folder → session listed; Resume → conversation continues;
  restart backend container → history + names persist.

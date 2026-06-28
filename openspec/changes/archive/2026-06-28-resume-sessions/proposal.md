# Proposal: Resume past sessions per folder

## Why

Conversations now persist (config is scoped to `~/.claude-sandbox`), but the
dashboard can only ever start a *fresh* session — there's no way to reopen a past
conversation. Users want to see a folder's previous sessions and resume one.

claude already supports this via its own flags (`--session-id`, `--resume`), so
we don't parse claude's transcript format. The dashboard just needs to own a
stable conversation id and offer a resume entry point.

## What Changes

- **Dashboard owns the conversation id.** On spawn, the backend generates a
  UUIDv4 and runs `claude --session-id <uuid> --dangerously-skip-permissions`.
  The uuid is recorded in the session's metadata sidecar.
- **Persisted session index.** A dashboard-owned JSON file in `$CLAUDE_CONFIG_DIR`
  (`~/.claude-sandbox/dashboard-sessions.json`) maps `uuid → {cwd, created,
  name}`. It is the source for the resume list. This replaces the old
  `session-names.json` (which lived in the non-persistent meta dir, so custom
  names were lost on container restart) — names now persist.
- **Rename is keyed by uuid.** Renaming a live session sets `index[uuid].name`
  (the live session's uuid comes from its metadata sidecar). The custom name then
  shows both in the sidebar and in the resume list.
- **Resume endpoint.** `GET /api/sessions/history?cwd=<path>` returns that
  folder's entries from the index. `POST /api/sessions` accepts an optional
  `resume=<uuid>`; when present the backend spawns a dtach session running
  `claude --resume <uuid>` in that folder instead of a new conversation.
- **New Session modal redesign** (per the approved mockup): navigate folders;
  "Start a new session" is pre-selected by default; a folder's previous sessions
  are listed below (label = custom name if set, else `<relative time> · <short
  uuid>`); selection is shown by background color only; a single fixed primary
  button relabels **Launch** ↔ **Resume** in place, with a permanent **Cancel**.

## Capabilities

### Modified Capabilities
- `dtach-sessions`: spawn assigns a claude conversation uuid via `--session-id`
  and records it; a persisted, uuid-keyed session index in `$CLAUDE_CONFIG_DIR`
  holds cwd/created/name; resume re-launches a recorded conversation.
- `session-api`: spawn accepts `resume`; new `GET /api/sessions/history`; rename
  is keyed by conversation uuid and persisted.
- `dashboard-ui`: the New Session modal browses folders and lists/resumes a
  folder's previous sessions.

## Impact

| File | Change |
|------|--------|
| `backend/paths.go` | `sessionMeta` gains `SessionID`; UUIDv4 helper; index path in `$CLAUDE_CONFIG_DIR` |
| `backend/session.go` | spawn `--session-id`; resume mode (`--resume`); session index read/write (replaces session-names); name + history lookups by uuid |
| `backend/handlers.go` | `handleSpawn` reads `resume`; new `handleHistory`; rename routes to index by uuid; register route |
| `frontend/handlers.go` | proxy `GET /api/sessions/history` |
| `frontend/web/templates/...` | New Session modal + directory-picker redesign |
| `frontend/web/static/js/views.js` | folder nav, history fetch, select-then-confirm, spawn/resume |

## Risks

- **Robustness:** the list comes from the dashboard's own index, not claude's
  storage, so a claude format/layout change cannot break it. Resume uses claude's
  `--resume <uuid>` flag; if a conversation can't be resumed, claude reports it —
  the dashboard never parses transcripts.
- **Pre-existing sessions** (spawned before this change) have no index entry and
  won't appear in the resume list. Acceptable — they predate the feature.
- **`--session-id` must be a valid UUID;** the backend generates a conformant
  UUIDv4.

## Decision

Proceed on the current working tree.

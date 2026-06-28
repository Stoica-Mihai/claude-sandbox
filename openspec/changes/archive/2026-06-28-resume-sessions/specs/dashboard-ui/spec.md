# Spec: dashboard-ui

**Spec Path:** `specs/dashboard-ui/spec.md`
**Change Type:** ADDED

---

## ADDED Requirements

### Requirement: New Session modal browses folders and resumes past sessions
The New Session modal SHALL let the user navigate directories under `/workspace` (clicking a folder enters it; the breadcrumb navigates back). Inside a folder the modal SHALL present a "Start a new session" option and a list of that folder's previous sessions fetched from `GET /api/sessions/history`. Each previous-session row SHALL be labeled with its custom name when set, otherwise its relative time and short uuid.

#### Scenario: Browse into a folder and see its sessions
- **WHEN** the user opens the modal and navigates into `/workspace/cmux`
- **THEN** the modal SHALL show "Start a new session" and the folder's previous sessions, each labeled by custom name or `<relative time> · <short uuid>`

#### Scenario: Folder with no previous sessions
- **WHEN** the user navigates into a folder that has no recorded sessions
- **THEN** the modal SHALL show a "no previous sessions" empty state under "Start a new session"

### Requirement: Select-then-confirm with a single morphing action
The modal SHALL indicate the chosen option by background color only (no radio control, no edge bar) and SHALL allow only one selection at a time. "Start a new session" SHALL be selected by default when a folder is entered. The footer SHALL contain a permanent Cancel button and a single primary action button that is always present and relabels in place: **Launch** when "Start a new session" is selected, **Resume** when a previous session is selected. At the `/workspace` root the primary button SHALL be present but disabled. Navigating to another folder SHALL reset the selection to the default.

#### Scenario: Default selection launches a new session
- **WHEN** the user enters a folder and clicks the primary button without changing the selection
- **THEN** the modal SHALL POST `{cwd}` to start a new session

#### Scenario: Selecting a previous session changes the action to Resume
- **WHEN** the user selects a previous-session row
- **THEN** that row SHALL be highlighted, the primary button SHALL read "Resume", and clicking it SHALL POST `{cwd, resume:<uuid>}`

#### Scenario: Primary action stays anchored
- **WHEN** the selection changes between "new" and a previous session
- **THEN** the primary button SHALL relabel in place without appearing/disappearing, and the Cancel button SHALL remain present and unmoved

## ADDED Requirements

### Requirement: Create a new project folder from the directory picker
The directory picker in the NEW SESSION modal SHALL provide a "+ NEW PROJECT…" affordance that lets the user create a new folder in the directory they are currently browsing and immediately proceed to launch a session in it. The affordance SHALL be a row pinned directly under the folder list and SHALL be present at every browse depth (workspace root and any subfolder), so the new folder is created inside the current breadcrumb path rather than only at the root.

Because the picker fragment is re-rendered on every drill-down and breadcrumb navigation, the affordance SHALL reset to its idle (collapsed) state after any such navigation; this reset is intended behavior.

The affordance SHALL be styled per the approved mockup and remain kit-conformant (square corners, 2px borders, a single accent): an idle `.newrow` rendered in the muted text color with an accent `+`, and an inline `.newedit` editor rendered on the `--field` background. Any divergence from the Futurism kit SHALL be recorded in the `app.css` override ledger.

#### Scenario: New-project row pinned at every depth
- **WHEN** the user opens the directory picker or drills into any subfolder
- **THEN** a "+ NEW PROJECT…" row SHALL be rendered directly beneath the folder list for the currently browsed directory

#### Scenario: Navigating away resets the editor
- **WHEN** the inline editor is open and the user navigates via a breadcrumb segment or drills into a subfolder
- **THEN** the picker fragment SHALL re-render and the affordance SHALL return to its idle collapsed "+ NEW PROJECT…" row

### Requirement: Inline new-project editor
Clicking the "+ NEW PROJECT…" row SHALL swap the row in place for an inline editor containing: a text input for the folder name that receives focus automatically, a "git init" checkbox that defaults to on, and CANCEL and CREATE actions. A hint line SHALL read "Enter to create · Esc to cancel". Pressing Enter in the name input SHALL trigger create; pressing Esc SHALL cancel and collapse back to the idle row. The Esc handler SHALL call `preventDefault` and `stopPropagation` so cancelling the editor does not also close the surrounding modal dialog (the native `<dialog>` Esc-cancel is a keydown default action, which `stopPropagation` alone cannot block).

#### Scenario: Open the editor
- **WHEN** the user clicks the "+ NEW PROJECT…" row
- **THEN** the row SHALL be replaced in place by the inline editor, the name input SHALL be focused, and the "git init" checkbox SHALL be checked by default

#### Scenario: Keyboard controls
- **WHEN** the editor is open and focused and the user presses Enter
- **THEN** the client SHALL attempt to create the project

#### Scenario: Esc cancels without closing the modal
- **WHEN** the editor is open and the user presses Esc
- **THEN** the editor SHALL collapse back to the idle "+ NEW PROJECT…" row, the event SHALL not propagate to the dialog, and the NEW SESSION modal SHALL remain open

### Requirement: Client-side new-project validation
Before sending a create request, the client SHALL pre-check the entered name against the same `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` regex the server enforces, and SHALL also check the name against the folder names currently listed in the picker to catch a duplicate. This client-side check is a UX convenience only — the server remains authoritative. On a failed pre-check the client SHALL show an inline error line and apply the existing `input.err-flash` accent outline to the name input, and SHALL NOT send the request. The inline error SHALL clear on the next keystroke in the name input.

#### Scenario: Reject an invalid name locally
- **WHEN** the user enters a name that fails the regex and triggers create
- **THEN** the client SHALL show the inline error "Invalid name" with the `input.err-flash` outline and SHALL NOT send a request

#### Scenario: Reject a duplicate name locally
- **WHEN** the user enters a name that matches a folder already listed in the picker and triggers create
- **THEN** the client SHALL show the inline error "Folder already exists" with the `input.err-flash` outline and SHALL NOT send a request

#### Scenario: Error clears on next keystroke
- **WHEN** an inline error is showing and the user types in the name input
- **THEN** the inline error line and the `input.err-flash` outline SHALL be removed

### Requirement: New project is created and a session auto-launched on success
On a create request that returns HTTP 201, the client SHALL spawn a session in the new folder immediately: a fresh folder has no conversations to resume, so a select-then-LAUNCH step would be a pointless extra click. The client sets the spawn form's hidden `cwd` to the new folder's full path, clears `resume`, and submits the existing spawn form — the spawn response's `X-Terminal-Id` handler then closes the modal and opens the terminal tab, identical to a manual LAUNCH. When the response is HTTP 201 with a `warning` field (folder created but `git init` failed), the client SHALL still launch the session and SHALL show the "created, git init failed" notice as a kit toast, since the notice must outlive the closing modal. When the server returns a `400` or `409`, the client SHALL surface the server's message inline using the same error affordance as the client-side pre-check, keeping the editor open. (For a plain folder-row click, the selected state SHALL show only the breadcrumb and session-actions: `dpSelectFolder` hides the folder list and the new-project affordance, and the browse reset restores them.)

#### Scenario: Successful creation launches a session
- **WHEN** the create request returns HTTP 201
- **THEN** the client SHALL submit the spawn form with the new folder as `cwd`, and on the spawn response the modal SHALL close and the new session's terminal tab SHALL open

#### Scenario: Creation with a git-init warning
- **WHEN** the create request returns HTTP 201 with a `warning` field
- **THEN** the client SHALL still launch the session and SHALL show a "created, git init failed" kit toast

#### Scenario: Server rejects a name the client did not catch
- **WHEN** the create request returns HTTP 409 (or 400)
- **THEN** the client SHALL display the server's message inline (for example "Folder already exists") with the `input.err-flash` outline and keep the editor open

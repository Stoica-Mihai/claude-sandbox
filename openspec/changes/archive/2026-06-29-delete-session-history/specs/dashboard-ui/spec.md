## ADDED Requirements

### Requirement: Delete a previous session from the resume list
Each previous-session row in the New Session modal's "Previous sessions" list SHALL carry an inline delete affordance. The delete affordance SHALL appear ONLY in the modal's resume list and SHALL NOT be added to the live sidebar session cards (which retain their existing Kill control). Because the resume row is itself a `<button>` and may not nest another interactive control, the row SHALL be wrapped in a non-interactive container (`div.arow-wrap`, `position: relative`) holding the existing resume `<button class="arow sa-row">` AND a SIBLING delete `<button class="arow-del">`. The delete control's icon SHALL be a `currentColor` stroke SVG or a text glyph — NOT an emoji.

The delete control's click handler SHALL call `e.stopPropagation()` and `e.preventDefault()` so it never triggers the row's resume-select handler (`dirPickerSetSel('resume', <uuid>, row)`).

Deletion SHALL require an inline two-step confirm (no native `window.confirm`). The first click SHALL swap the `.arow-del` control's contents into an accent confirm + ghost cancel pair, styled with existing Futurism tokens (`--accent` for confirm, `--muted`/`--line` for the ghost cancel) and no hardcoded hex; the resume button markup SHALL remain untouched. New styles SHALL be added to `app.css` only (never `futurism.css`).

Confirming SHALL issue `fetch("/api/sessions/history/" + uuid, { method: "DELETE" })`. On HTTP 204 the modal SHALL re-fetch `GET /api/sessions/history?cwd=<path>` for the current folder and re-render the Previous-sessions list (the re-fetch-and-render is the source of truth for the modal — the modal SHALL NOT rely on the SSE/broker update to refresh its list), so the deleted row disappears. On a non-204 response (e.g. 404) the inline confirm SHALL revert to the idle delete affordance and surface a brief on-brand failure indication (a transient text/class swap on the control — no `window.alert`).

#### Scenario: Delete a previous session row
- **WHEN** the user clicks the delete control on a previous-session row, then clicks the inline confirm, and the server responds 204
- **THEN** the row's `DELETE /api/sessions/history/{uuid}` SHALL be issued, the folder's history SHALL be re-fetched and re-rendered, and the deleted row SHALL disappear from the list

#### Scenario: Delete control does not trigger resume-select
- **WHEN** the user clicks the delete control (or its confirm/cancel) on a previous-session row
- **THEN** `stopPropagation`/`preventDefault` SHALL prevent the row's resume-select handler from firing, so the primary action does NOT change to "Resume" and no resume selection occurs

#### Scenario: Cancel the inline confirm
- **WHEN** the user clicks the delete control and then clicks the inline cancel
- **THEN** the control SHALL revert to the idle delete affordance and no DELETE request SHALL be issued

#### Scenario: Delete failure reverts and surfaces feedback
- **WHEN** the user confirms deletion and the server responds with a non-204 status
- **THEN** the inline confirm SHALL revert to the idle delete affordance and a brief on-brand failure indication SHALL be shown without any `window.alert` or `window.confirm`

#### Scenario: Sidebar cards are unaffected
- **WHEN** the delete affordance is present in the resume list
- **THEN** the live sidebar session cards SHALL continue to show only their existing Kill control and SHALL NOT gain a history-delete control

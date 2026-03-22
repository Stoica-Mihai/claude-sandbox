# Spec: Dashboard UI — Keyboard Shortcuts

**Spec Path:** `specs/dashboard-ui/spec.md`
**Change Type:** ADDED

---

## ADDED Requirements

### Requirement: Dashboard supports keyboard shortcuts for tab and view management

The dashboard SHALL provide keyboard shortcuts for common tab and view operations, allowing power users to manage sessions without using the mouse.

#### Scenario: Open new session modal with Ctrl+T

- **WHEN** the user presses Ctrl+T
- **AND** no modal dialog is currently open
- **AND** focus is not in a non-terminal input field
- **THEN** the new session modal is displayed
- **THEN** the default browser new-tab action is prevented (where browser policy allows)

#### Scenario: Close current tab with Ctrl+W

- **WHEN** the user presses Ctrl+W
- **AND** at least one session tab is open
- **AND** no modal dialog is currently open
- **THEN** the currently active session tab is closed
- **THEN** if the session is still running, a confirmation prompt is shown before closing
- **THEN** the default browser close-tab action is prevented (where browser policy allows)

#### Scenario: Switch to tab by index with Ctrl+1 through Ctrl+9

- **WHEN** the user presses Ctrl+N where N is a digit from 1 to 9
- **AND** there are at least N open tabs
- **THEN** the Nth tab becomes the active tab (1-indexed)
- **THEN** the corresponding terminal receives focus

#### Scenario: Tab index exceeds open tab count

- **WHEN** the user presses Ctrl+N where N exceeds the number of open tabs
- **THEN** no action is taken
- **THEN** no error is shown

#### Scenario: Toggle split view with Ctrl+\

- **WHEN** the user presses Ctrl+\
- **AND** at least two session tabs are open
- **THEN** split view is toggled (enabled if disabled, disabled if enabled)

#### Scenario: Shortcuts suppressed when modal is open

- **WHEN** any modal dialog is visible
- **AND** the user presses any registered shortcut key combination
- **THEN** the shortcut action is NOT executed
- **THEN** the keypress is handled normally by the modal

#### Scenario: Shortcuts suppressed in non-terminal input fields

- **WHEN** focus is on an `<input>` or `<textarea>` element that is not the terminal or mobile input bar
- **AND** the user presses a registered shortcut key combination
- **THEN** the shortcut action is NOT executed

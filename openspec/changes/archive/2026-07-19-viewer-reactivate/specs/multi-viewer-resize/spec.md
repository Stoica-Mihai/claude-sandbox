## ADDED Requirements

### Requirement: Passive reactivation on focus
A suspended viewer SHALL be able to become the active viewer without injecting terminal input. When a suspended viewer's terminal gains focus, the client SHALL send a `reactivate` control message (a JSON text message, never a binary/input frame). On receipt sessiond SHALL make that viewer the active one — resizing the PTY to its dimensions and suspending the others per the active-viewer policy — and SHALL push that viewer a fresh terminal snapshot rendered at its dimensions, so its display becomes live immediately with no byte written to the session PTY.

#### Scenario: Focusing a suspended viewer takes the live view
- **WHEN** a suspended viewer's terminal gains focus and it sends a `reactivate` control message
- **THEN** sessiond SHALL make it the active viewer, suspend the previously active viewer (which receives a `deactivated` message), and send the reactivated viewer a fresh snapshot — without writing any input to the PTY

#### Scenario: Reactivate injects no input
- **WHEN** a viewer reactivates by focus
- **THEN** no byte SHALL be written to the session PTY as a result (the claude process sees no keystroke)

#### Scenario: Reactivate from an unattached or unknown connection is a no-op
- **WHEN** a `reactivate` control message arrives for a connection that is not a registered viewer
- **THEN** sessiond SHALL ignore it, changing neither the active viewer nor the PTY size

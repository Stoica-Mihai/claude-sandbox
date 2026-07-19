## ADDED Requirements

### Requirement: Reactivate control op
sessiond SHALL accept a `reactivate` CONTROL frame on a session stream. It SHALL make the sending viewer active (via the active-viewer policy: resize the PTY to the viewer's dimensions, suspend the others, notify them), then send that viewer a fresh SNAPSHOT frame rendered at its dimensions. A `reactivate` from a connection that is not a registered viewer SHALL be ignored. Unlike a DATA frame, it SHALL write nothing to the PTY.

#### Scenario: Reactivate makes the requester active and repaints it
- **WHEN** a registered but suspended viewer sends a `reactivate` CONTROL frame
- **THEN** sessiond SHALL set it active, resize the PTY to its dimensions, suspend and notify the previously active viewer, and enqueue it a fresh SNAPSHOT frame

#### Scenario: Reactivate writes nothing to the PTY
- **WHEN** sessiond handles a `reactivate` frame
- **THEN** it SHALL NOT write to the session PTY

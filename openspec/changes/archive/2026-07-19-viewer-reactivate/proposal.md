## Why

Multi-viewer sessions reactivate the "active viewer" only when a viewer **sends terminal input**. Focusing a suspended viewer (tapping the terminal on a phone to raise the keyboard) sends nothing, so the screen stays frozen until the first keystroke — and that keystroke is injected into claude. There is no way to take the live view passively: to *look* on a second device you must also type a character. Reported on a phone+desktop pair sharing one session.

## What Changes

- New `reactivate` control message: a suspended viewer requests the live view without injecting input. The client sends it when a suspended terminal gains focus; sessiond makes that viewer active (resizing the PTY to its dimensions, suspending the others) and pushes it a fresh emulator snapshot rendered at its size.
- `SessionSocket` gains a generic `sendControl(msg)`; `sendResize` delegates to it.
- The WS→sessiond bridge forwards `reactivate` control frames (today it forwards only `resize`).

## Capabilities

### Modified Capabilities

- `multi-viewer-resize`: adds passive reactivation — focus, not just input, can make a viewer active.
- `session-host`: sessiond handles a `reactivate` protocol control op (activate + fresh snapshot to the requester).

## Impact

- `sessiond/protocol` (new `ControlReactivate` constant), `sessiond/session.go` (cmd + handler), `backend/handlers.go` (bridge forwards it), `frontend/web/static/js/session-socket.js` (`sendControl`), `frontend/web/static/js/terminal.js` (focus → reactivate).
- Wire-compatible: a new control `type` value only; no frame-shape change. Clients that never send it behave exactly as before.

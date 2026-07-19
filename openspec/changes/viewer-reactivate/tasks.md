## 1. Protocol + sessiond

- [x] 1.1 Add `ControlReactivate = "reactivate"` to `sessiond/protocol`
- [x] 1.2 sessiond: `cmdReactivate{conn}`; `readConn` maps a `reactivate` CONTROL frame to it; handler unsuspends + activates the viewer and enqueues a fresh snapshot; unknown conn is a no-op
- [x] 1.3 Test: two viewers, the suspended one sends reactivate → it gets a snapshot + becomes active, the other gets `deactivated`; assert no PTY write

## 2. Backend bridge

- [x] 2.1 `bridgeWSToFrames` forwards a `reactivate` control frame to sessiond (after attach); keep resize's first-message ATTACH behavior
- [x] 2.2 Test: after attach, a `reactivate` text message arrives at the fake session socket as a CONTROL frame

## 3. Frontend

- [x] 3.1 `SessionSocket.sendControl(msg)`; `sendResize` delegates to it
- [x] 3.2 `terminal.js`: on `focusin` of a terminal whose `needsRefresh` is set, clear the flag and `sendControl({type:'reactivate'})` instead of waiting for a keystroke
- [x] 3.3 Frontend tests green (session-socket + views)

## 4. Verify

- [x] 4.1 `go test -race` (backend + sessiond) and JS tests green
- [x] 4.2 Live: two viewers on one session; type on A; focus B (no typing) → B repaints live, A freezes; confirm claude saw no stray input

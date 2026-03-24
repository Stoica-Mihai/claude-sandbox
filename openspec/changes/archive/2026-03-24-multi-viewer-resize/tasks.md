## 1. Viewer Struct and Relay Fields

- [x] 1.1 Add `viewerSize` struct with `cols` and `rows` fields to `relay.go`
- [x] 1.2 Add `viewer` struct with `writeMu sync.Mutex`, `size viewerSize`, and `suspended atomic.Bool` to `relay.go`
- [x] 1.3 Replace `viewers map[*websocket.Conn]struct{}` with `viewers map[*websocket.Conn]*viewer` in `Relay` struct
- [x] 1.4 Add `lastResizer *websocket.Conn` field to `Relay` struct
- [x] 1.5 Add `sync/atomic` to imports
- [x] 1.6 Update `NewRelay` to initialize `viewers` as `map[*websocket.Conn]*viewer`

## 2. Viewer Lifecycle

- [x] 2.1 Update `AddViewer` to create `&viewer{}` when adding to the map
- [x] 2.2 Update `RemoveViewer` to clear `lastResizer` if the removed viewer was the active one
- [x] 2.3 Update `Stop` to acquire `v.writeMu` before writing close frames to each viewer

## 3. Resize Methods

- [x] 3.1 Implement `Resize(conn, cols, rows)` — store dimensions in `v.size`, set `lastResizer` on first viewer only, resize tmux only if `isActive`
- [x] 3.2 Implement `ResizeToViewer(conn)` — short-circuit if already active, set `lastResizer`, suspend non-active viewers, send `deactivatedMsg`, resize tmux
- [x] 3.3 Implement `UnsuspendViewer(conn)` — set `v.suspended.Store(false)`
- [x] 3.4 Define `deactivatedMsg` as `[]byte(`{"type":"deactivated"}`)`

## 4. Broadcast Changes

- [x] 4.1 Update `broadcast` to skip viewers where `v.suspended.Load()` is true
- [x] 4.2 Update `broadcast` to acquire `v.writeMu` before writing and release after

## 5. WebSocket Handler Changes

- [x] 5.1 Rename `resizeMessage` to `controlMessage` with `Type`, `Cols`, `Rows` fields in `handlers.go`
- [x] 5.2 Update TextMessage handling to parse `controlMessage` and call `relay.Resize(conn, cols, rows)` for resize type
- [x] 5.3 Update BinaryMessage handling to call `relay.UnsuspendViewer(conn)` → `relay.ResizeToViewer(conn)` → `relay.SendInput(data)`

## 6. Client-Side Changes

- [x] 6.1 Add `needsRefresh` flag in `terminal.js` WebSocket scope
- [x] 6.2 Update `ws.onmessage` to parse text messages as JSON control messages; set `needsRefresh = true` on `"deactivated"` type
- [x] 6.3 Update `term.onData` to check `needsRefresh` — if true, set to false and call `term.clear()` before sending input

## 7. Verification

- [x] 7.1 `go build` and `go vet` pass
- [x] 7.2 Docker image builds successfully
- [x] 7.3 Single viewer: terminal is interactive, resize works
- [x] 7.4 Two viewers same session: active viewer gets correct display
- [x] 7.5 Two viewers: non-active viewer display freezes (no garbled content)
- [x] 7.6 Two viewers: typing on frozen viewer clears and shows correct content
- [x] 7.7 Viewer disconnect: remaining viewer continues working, can become active

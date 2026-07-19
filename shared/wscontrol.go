package api

// WS/protocol control-message type values — the vocabulary the browser JS, the
// backend bridge, and sessiond all speak over the WS text / CONTROL frame
// channel. This is the single owner: sessiond/protocol aliases these (its Go
// consumers keep referencing protocol.Control*), and the frontend injects them
// into the browser as window.WS_CONTROL, so no side re-types the literals.
const (
	WSControlResize      = "resize"
	WSControlDeactivated = "deactivated"
	WSControlReactivate  = "reactivate"
	WSControlError       = "error"
)

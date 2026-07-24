package api

import "time"

// SessiondHealthPort is the internal port sessiond serves /healthz on (its only
// TCP surface; the control socket is unix-domain). Single-sourced so sessiond's
// bind default, logd's probe target, and compose agree.
const SessiondHealthPort = "8083"

// Service state values reported by the health monitor.
const (
	ServiceUp   = "up"
	ServiceDown = "down"
)

// ServiceStatus is logd's aggregated health view of one service, served by the
// status API and streamed over SSE. LastLogSeen is nil until logd has ingested
// a line from that service.
type ServiceStatus struct {
	Service     string     `json:"service"`
	State       string     `json:"state"`
	Since       time.Time  `json:"since"`
	LastLogSeen *time.Time `json:"lastLogSeen"`
}

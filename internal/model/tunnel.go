package model

import "time"

// TunnelState is the runtime state of a wan service.
type TunnelState string

const (
	TunnelStopped TunnelState = "stopped"
	TunnelRunning TunnelState = "running"
	TunnelError   TunnelState = "error"
)

// TunnelEntry maps a short 5-char share code to a full wan invocation.
// Entries are persisted in the tunnel registry and may be shared over the
// cluster protocol so a peer can resolve a code into a runnable command.
type TunnelEntry struct {
	Code        string      `json:"code"` // exactly 5 chars
	ServerID    string      `json:"serverId,omitempty"`
	FullCommand string      `json:"fullCommand"`
	TunnelID    string      `json:"tunnelId,omitempty"`
	Port        int         `json:"port"`
	Hostname    string      `json:"hostname,omitempty"`
	Autostart   bool        `json:"autostart"`
	ServiceMode bool        `json:"serviceMode"`
	State       TunnelState `json:"state"`
	LastLog     string      `json:"lastLog,omitempty"`
	Error       string      `json:"error,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
}

// TunnelRegistry is the persisted set of share codes.
type TunnelRegistry struct {
	Entries map[string]*TunnelEntry `json:"entries"` // keyed by code
}

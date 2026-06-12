package model

import "time"

// PeerState is the availability of a paired peer.
type PeerState string

const (
	PeerAvailable PeerState = "available"
	PeerBusy      PeerState = "busy"
	PeerOffline   PeerState = "offline"
)

// Peer is another MCOS node discovered/paired on the LAN.
type Peer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	IP         string    `json:"ip"`
	Port       int       `json:"port"`
	CPUModel   string    `json:"cpuModel"`
	Cores      int       `json:"cores"`
	RAMMB      int       `json:"ramMB"`
	GPU        string    `json:"gpu,omitempty"`
	FreeRAMMB  int       `json:"freeRamMB"`
	FreeCPUPct float64   `json:"freeCpuPct"`
	State      PeerState `json:"state"`
	Paired     bool      `json:"paired"`
	ActiveJobs int       `json:"activeJobs"`
	LastSeen   time.Time `json:"lastSeen"`
}

// TaskKind categorizes a distributable unit of work.
type TaskKind string

const (
	TaskGameHost    TaskKind = "game-host"
	TaskBackup      TaskKind = "backup"
	TaskLogAnalysis TaskKind = "log-analysis"
	TaskTunnel      TaskKind = "tunnel"
	TaskFileOp      TaskKind = "file-op"
)

// TaskState tracks a task through its lifecycle.
type TaskState string

const (
	TaskQueued    TaskState = "queued"
	TaskRunning   TaskState = "running"
	TaskDone      TaskState = "done"
	TaskFailed    TaskState = "failed"
	TaskMigrating TaskState = "migrating"
)

// Task is a unit of work that may run locally or be handed to a peer.
type Task struct {
	ID         string            `json:"id"`
	Kind       TaskKind          `json:"kind"`
	State      TaskState         `json:"state"`
	ServerID   string            `json:"serverId,omitempty"`
	AssignedTo string            `json:"assignedTo"` // peer ID or "" for local
	Params     map[string]string `json:"params,omitempty"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	Result     string            `json:"result,omitempty"` // human-readable outcome on success
	Error      string            `json:"error,omitempty"`
}

package supervisor

import "errors"

// ErrUnknown is returned when a process name is not registered.
var ErrUnknown = errors.New("supervisor: unknown process")

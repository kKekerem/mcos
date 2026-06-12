package panel

import "mcos/internal/model"

// Local aliases keep the view code terse while reusing the shared model types
// that come back over the wire.
type (
	systemStatus = model.SystemStatus
	serverInfo   = model.Server
	javaRuntime  = model.JavaRuntime
)

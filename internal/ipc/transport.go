package ipc

import (
	"fmt"
	"net"
	"runtime"
	"strings"
)

// ParseAddr splits an endpoint string of the form "unix:///run/mcos/mcosd.sock"
// or "tcp://127.0.0.1:7777" into a (network, address) pair usable with net.Dial
// and net.Listen.
func ParseAddr(endpoint string) (network, address string, err error) {
	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		return "unix", strings.TrimPrefix(endpoint, "unix://"), nil
	case strings.HasPrefix(endpoint, "tcp://"):
		return "tcp", strings.TrimPrefix(endpoint, "tcp://"), nil
	default:
		return "", "", fmt.Errorf("ipc: unsupported endpoint %q (use unix:// or tcp://)", endpoint)
	}
}

// DefaultEndpoint returns the platform-appropriate default IPC endpoint. On
// Linux (the OS target) it is the Unix socket; on Windows dev hosts it falls
// back to TCP loopback for convenience.
func DefaultEndpoint() string {
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:7777"
	}
	return "unix:///run/mcos/mcosd.sock"
}

// Listen opens a listener for the given endpoint, removing a stale unix socket
// file if one exists.
func Listen(endpoint string) (net.Listener, error) {
	network, address, err := ParseAddr(endpoint)
	if err != nil {
		return nil, err
	}
	if network == "unix" {
		// Best-effort cleanup of a leftover socket from a previous run.
		_ = removeIfSocket(address)
	}
	return net.Listen(network, address)
}

// Dial connects to the given endpoint.
func Dial(endpoint string) (net.Conn, error) {
	network, address, err := ParseAddr(endpoint)
	if err != nil {
		return nil, err
	}
	return net.Dial(network, address)
}

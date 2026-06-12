// Package players implements player-list discovery and command proxying by
// sending Minecraft console commands and parsing the resulting output. It does
// not require RCON; everything goes through the supervised process stdin/stdout.
package players

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"mcos/internal/ipc"
	"mcos/internal/model"
	"mcos/internal/server"
)

// Manager proxies player operations through the server console.
type Manager struct {
	servers *server.Manager
}

// NewManager constructs the players manager.
func NewManager(sm *server.Manager) *Manager {
	return &Manager{servers: sm}
}

var listRe = regexp.MustCompile(`There are (\d+) of a max of (\d+) players online:? ?(.*)`)

// List sends the "list" command, waits briefly for output, then parses the
// last console lines for the player count and names.
func (m *Manager) List(serverID string) (*ipc.PlayersListResult, error) {
	if err := m.servers.Command(serverID, "list"); err != nil {
		return nil, fmt.Errorf("send list: %w", err)
	}
	time.Sleep(400 * time.Millisecond)

	lines := m.servers.TailConsole(serverID, 20)
	result := &ipc.PlayersListResult{}

	for i := len(lines) - 1; i >= 0; i-- {
		text := lines[i].Message
		if m := listRe.FindStringSubmatch(text); m != nil {
			result.Online = atoi(m[1])
			result.Max = atoi(m[2])
			if m[3] != "" {
				names := strings.Split(m[3], ",")
				for _, n := range names {
					n = strings.TrimSpace(n)
					if n != "" {
						result.Players = append(result.Players, model.PlayerInfo{Name: n, Online: true})
					}
				}
			}
			break
		}
	}
	return result, nil
}

// Command sends a raw players-related command to the server console.
func (m *Manager) Command(serverID, command string) error {
	return m.servers.Command(serverID, command)
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

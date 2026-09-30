package store

import "path/filepath"

// Paths centralizes every on-disk location the daemon uses, all rooted under a
// single data root so the whole state tree is portable and backup-friendly.
type Paths struct {
	Root string
}

func (p Paths) Servers() string            { return filepath.Join(p.Root, "servers") }
func (p Paths) ServerDir(id string) string { return filepath.Join(p.Root, "servers", id) }
func (p Paths) ServerManifest(id string) string {
	return filepath.Join(p.ServerDir(id), "manifest.json")
}
func (p Paths) ServerData(id string) string    { return filepath.Join(p.ServerDir(id), "data") }
func (p Paths) ServerLogs(id string) string    { return filepath.Join(p.ServerDir(id), "logs") }
func (p Paths) ServerBackups(id string) string { return filepath.Join(p.ServerDir(id), "backups") }

func (p Paths) JavaDir() string   { return filepath.Join(p.Root, "java") }
func (p Paths) JavaIndex() string { return filepath.Join(p.JavaDir(), "index.json") }

func (p Paths) ClusterDir() string { return filepath.Join(p.Root, "cluster") }
func (p Paths) PeersFile() string  { return filepath.Join(p.ClusterDir(), "peers.json") }
func (p Paths) NodeIDFile() string { return filepath.Join(p.ClusterDir(), "node.id") }

func (p Paths) TunnelsDir() string     { return filepath.Join(p.Root, "tunnels") }
func (p Paths) TunnelRegistry() string { return filepath.Join(p.TunnelsDir(), "registry.json") }

func (p Paths) LogDir() string { return filepath.Join(p.Root, "log") }
func (p Paths) RunDir() string { return filepath.Join(p.Root, "run") }

// Package providers resolves and downloads Minecraft server software for each
// supported flavor (Vanilla, Paper, Purpur, Fabric, Quilt, Forge, NeoForge,
// Folia, CraftBukkit). Each provider knows how to fetch the right artifact for
// a Minecraft version and how the resulting server should be launched.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"mcos/internal/log"
	"mcos/internal/model"
)

// InstallResult tells the lifecycle layer how to launch the installed server.
// Exactly one of JarFile / ArgsFile / Script is the primary launch mechanism.
type InstallResult struct {
	JarFile    string   `json:"jarFile,omitempty"`    // launched with: java <jvm> -jar <JarFile> <LaunchArgs>
	ArgsFile   string   `json:"argsFile,omitempty"`   // launched with: java <jvm> @<ArgsFile> <LaunchArgs>
	Script     string   `json:"script,omitempty"`     // launched by executing this script
	LaunchArgs []string `json:"launchArgs,omitempty"` // trailing args, typically ["nogui"]
	Notes      string   `json:"notes,omitempty"`
}

// Provider installs one server flavor. javaBin is the resolved java executable
// for this server's Minecraft version; flavors whose installation runs an
// installer (Forge, NeoForge, Quilt, Spigot/CraftBukkit BuildTools) use it,
// while jar-only flavors (Vanilla, Paper, Purpur, Fabric) ignore it.
type Provider interface {
	Software() model.Software
	Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error)
}

// registry holds all known providers.
var registry = map[model.Software]Provider{}

func register(p Provider) { registry[p.Software()] = p }

// Get returns the provider for a software flavor, or false.
func Get(s model.Software) (Provider, bool) {
	p, ok := registry[s]
	return p, ok
}

func init() {
	register(vanillaProvider{})
	register(paperLikeProvider{software: model.SoftwarePaper, project: "paper"})
	register(paperLikeProvider{software: model.SoftwareFolia, project: "folia"})
	register(purpurProvider{})
	register(fabricProvider{})
	register(quiltProvider{})
	register(forgeProvider{software: model.SoftwareForge})
	register(forgeProvider{software: model.SoftwareNeoForge})
	register(buildToolsProvider{software: model.SoftwareSpigot, target: "spigot"})
	register(buildToolsProvider{software: model.SoftwareCraftBukkit, target: "craftbukkit"})
}

// --- shared HTTP helpers ---------------------------------------------------

// getJSON fetches url and decodes the JSON body into v.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// downloadTo streams url into dir/filename and returns the absolute path.
func downloadTo(ctx context.Context, client *http.Client, url, dir, filename string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "mcos/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %s", url, resp.Status)
	}
	dst := filepath.Join(dir, filename)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return dst, nil
}

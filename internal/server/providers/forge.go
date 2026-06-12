package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mcos/internal/log"
	"mcos/internal/model"
)

// forgeProvider installs Forge or NeoForge. Both ship an installer jar that,
// run with --installServer, lays out libraries and a modern args file
// (libraries/.../unix_args.txt) used to launch the server.
type forgeProvider struct {
	software model.Software
}

func (p forgeProvider) Software() model.Software { return p.software }

func (p forgeProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	installerURL, label, err := p.installerURL(ctx, client, mcVersion)
	if err != nil {
		return nil, err
	}
	if lg != nil {
		lg.Infof("%s: downloading installer %s", p.software, label)
	}
	installer, err := downloadTo(ctx, client, installerURL, dir, "installer.jar")
	if err != nil {
		return nil, err
	}
	defer os.Remove(installer)

	if lg != nil {
		lg.Infof("%s: running --installServer (this can take a minute)", p.software)
	}
	if _, err := runJava(ctx, javaBin, dir, lg, "-jar", "installer.jar", "--installServer"); err != nil {
		return nil, err
	}

	// Modern (>=1.17): an args file under libraries/.
	if argsFile := findArgsFile(dir); argsFile != "" {
		rel, _ := filepath.Rel(dir, argsFile)
		return &InstallResult{ArgsFile: filepath.ToSlash(rel), LaunchArgs: []string{"nogui"}}, nil
	}
	// Legacy: a universal/server jar produced in place.
	if jar := findForgeJar(dir); jar != "" {
		return &InstallResult{JarFile: jar, LaunchArgs: []string{"nogui"}}, nil
	}
	return nil, fmt.Errorf("%s: could not locate launch args or jar after install", p.software)
}

func (p forgeProvider) installerURL(ctx context.Context, client *http.Client, mc string) (url, label string, err error) {
	if p.software == model.SoftwareNeoForge {
		return neoforgeInstaller(ctx, client, mc)
	}
	return forgeInstaller(ctx, client, mc)
}

// --- Forge ---------------------------------------------------------------

const forgePromos = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
const forgeMaven = "https://maven.minecraftforge.net/net/minecraftforge/forge"

func forgeInstaller(ctx context.Context, client *http.Client, mc string) (string, string, error) {
	var promos struct {
		Promos map[string]string `json:"promos"`
	}
	if err := getJSON(ctx, client, forgePromos, &promos); err != nil {
		return "", "", fmt.Errorf("forge: promotions: %w", err)
	}
	forgeVer := promos.Promos[mc+"-recommended"]
	if forgeVer == "" {
		forgeVer = promos.Promos[mc+"-latest"]
	}
	if forgeVer == "" {
		return "", "", fmt.Errorf("forge: no build for MC %q", mc)
	}
	full := mc + "-" + forgeVer
	url := fmt.Sprintf("%s/%s/forge-%s-installer.jar", forgeMaven, full, full)
	return url, full, nil
}

// --- NeoForge ------------------------------------------------------------

const neoforgeVersionsAPI = "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge"
const neoforgeMaven = "https://maven.neoforged.net/releases/net/neoforged/neoforge"

func neoforgeInstaller(ctx context.Context, client *http.Client, mc string) (string, string, error) {
	prefix, err := neoVersionPrefix(mc)
	if err != nil {
		return "", "", err
	}
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, client, neoforgeVersionsAPI, &resp); err != nil {
		return "", "", fmt.Errorf("neoforge: versions: %w", err)
	}
	var matches []string
	for _, v := range resp.Versions {
		if strings.HasPrefix(v, prefix+".") {
			matches = append(matches, v)
		}
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("neoforge: no build for MC %q (prefix %s)", mc, prefix)
	}
	sort.Strings(matches)
	ver := matches[len(matches)-1]
	url := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", neoforgeMaven, ver, ver)
	return url, ver, nil
}

// neoVersionPrefix maps an MC version to the NeoForge version prefix:
// 1.21.1 -> "21.1", 1.21 -> "21.0", 1.20.4 -> "20.4".
func neoVersionPrefix(mc string) (string, error) {
	parts := strings.Split(mc, ".")
	if len(parts) < 2 || parts[0] != "1" {
		return "", fmt.Errorf("neoforge: unsupported MC version %q", mc)
	}
	minor := parts[1]
	patch := "0"
	if len(parts) >= 3 {
		patch = parts[2]
	}
	return minor + "." + patch, nil
}

// --- launch artifact discovery ------------------------------------------

func findArgsFile(dir string) string {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() && d.Name() == "unix_args.txt" {
			found = path
		}
		return nil
	})
	return found
}

func findForgeJar(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "forge-") && strings.HasSuffix(name, ".jar") && !strings.Contains(name, "installer") {
			return name
		}
	}
	return ""
}

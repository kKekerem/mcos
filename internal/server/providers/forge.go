package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mcos/internal/log"
	"mcos/internal/mcver"
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
	// Eski dallarda maven adı ek taşıyor (1.7.10 -> "…-1.7.10"): bkz.
	// forge_maven.go.
	full := forgeMavenSurumu(ctx, client, mc+"-"+forgeVer)
	url := fmt.Sprintf("%s/%s/forge-%s-installer.jar", forgeMaven, full, full)
	return url, full, nil
}

// --- NeoForge ------------------------------------------------------------

const neoforgeVersionsAPI = "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge"
const neoforgeMaven = "https://maven.neoforged.net/releases/net/neoforged/neoforge"

func neoforgeInstaller(ctx context.Context, client *http.Client, mc string) (string, string, error) {
	prefix, ok := mcver.NeoForgePrefix(mc)
	if !ok {
		return "", "", fmt.Errorf("neoforge: Minecraft %q için NeoForge yok "+
			"(NeoForge 1.20.2 ve sonrasının tam sürümlerini destekler)", mc)
	}
	var resp struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON(ctx, client, neoforgeVersionsAPI, &resp); err != nil {
		return "", "", fmt.Errorf("neoforge: versions: %w", err)
	}
	ver, ok := pickNeoForge(resp.Versions, prefix)
	if !ok {
		return "", "", fmt.Errorf("neoforge: no build for MC %q (prefix %s)", mc, prefix)
	}
	url := fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", neoforgeMaven, ver, ver)
	return url, ver, nil
}

// pickNeoForge chooses the build for a NeoForge prefix: stable over beta over
// alpha, then the highest build NUMBER.
//
// ── Yakalanan gerçek hata: metin sıralaması eski derleme seçiyordu ──────────
// Eşleşenler sort.Strings ile sıralanıp sonuncusu alınıyordu. Gerçek sürüm
// listesiyle (maven.neoforged.net, 2026-09-27):
//
//	1.21.1  -> 21.1.99 seçiliyordu, en yenisi 21.1.252 ("99" > "252" metin)
//	1.21.11 -> 21.11.9-beta seçiliyordu, kararlı 21.11.45 varken
//
// Ayrıca 26.x'in "26.1.0.0-alpha.1+snapshot-1" gibi derlemeleri Minecraft
// ANLIK GÖRÜNTÜLERİ içindir; tam sürüm sunucusuna kurulmaz, atlanır. 26.3'te
// henüz yalnızca "-beta" derlemeler var: kararlı yoksa en yeni beta alınır.
func pickNeoForge(all []string, prefix string) (string, bool) {
	best, bestRank := "", 99
	for _, v := range all {
		rest, ok := strings.CutPrefix(v, prefix+".")
		if !ok {
			continue
		}
		num, suffix := rest, ""
		if i := strings.IndexAny(rest, "-+"); i >= 0 {
			num, suffix = rest[:i], rest[i:]
		}
		// Önekten sonra TEK bir derleme numarası olmalı: "21.1" öneki
		// "21.1.3.4" gibi başka bir düzeni yakalamasın.
		if _, err := strconv.Atoi(num); err != nil || strings.Contains(suffix, "+") {
			continue
		}
		rank := 3
		switch {
		case suffix == "":
			rank = 0
		case strings.HasPrefix(suffix, "-beta"):
			rank = 1
		case strings.HasPrefix(suffix, "-alpha"):
			rank = 2
		}
		if best == "" || rank < bestRank || (rank == bestRank && mcver.CompareNumeric(v, best) > 0) {
			best, bestRank = v, rank
		}
	}
	return best, best != ""
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

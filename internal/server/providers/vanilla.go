package providers

import (
	"context"
	"fmt"
	"net/http"

	"mcos/internal/log"
	"mcos/internal/model"
)

const mojangManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

type vanillaProvider struct{}

func (vanillaProvider) Software() model.Software { return model.SoftwareVanilla }

type mojangManifest struct {
	Versions []struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	} `json:"versions"`
}

type mojangVersion struct {
	Downloads struct {
		Server struct {
			URL string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
}

func (vanillaProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	var manifest mojangManifest
	if err := getJSON(ctx, client, mojangManifestURL, &manifest); err != nil {
		return nil, fmt.Errorf("vanilla: manifest: %w", err)
	}
	verURL := ""
	for _, v := range manifest.Versions {
		if v.ID == mcVersion {
			verURL = v.URL
			break
		}
	}
	if verURL == "" {
		return nil, fmt.Errorf("vanilla: version %q not found in manifest", mcVersion)
	}
	var ver mojangVersion
	if err := getJSON(ctx, client, verURL, &ver); err != nil {
		return nil, fmt.Errorf("vanilla: version meta: %w", err)
	}
	if ver.Downloads.Server.URL == "" {
		return nil, fmt.Errorf("vanilla: no server download for %q (too old?)", mcVersion)
	}
	if lg != nil {
		lg.Infof("vanilla: downloading server.jar for %s", mcVersion)
	}
	if _, err := downloadTo(ctx, client, ver.Downloads.Server.URL, dir, "server.jar"); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

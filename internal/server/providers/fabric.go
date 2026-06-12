package providers

import (
	"context"
	"fmt"
	"net/http"

	"mcos/internal/log"
	"mcos/internal/model"
)

const fabricMeta = "https://meta.fabricmc.net/v2"

type fabricProvider struct{}

func (fabricProvider) Software() model.Software { return model.SoftwareFabric }

type fabricLoaderEntry struct {
	Loader struct {
		Version string `json:"version"`
	} `json:"loader"`
}

type fabricInstallerEntry struct {
	Version string `json:"version"`
}

func (fabricProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	var loaders []fabricLoaderEntry
	if err := getJSON(ctx, client, fmt.Sprintf("%s/versions/loader/%s", fabricMeta, mcVersion), &loaders); err != nil {
		return nil, fmt.Errorf("fabric: loaders for %q: %w", mcVersion, err)
	}
	if len(loaders) == 0 {
		return nil, fmt.Errorf("fabric: no loader for %q", mcVersion)
	}
	loader := loaders[0].Loader.Version // first = latest stable

	var installers []fabricInstallerEntry
	if err := getJSON(ctx, client, fmt.Sprintf("%s/versions/installer", fabricMeta), &installers); err != nil {
		return nil, fmt.Errorf("fabric: installers: %w", err)
	}
	if len(installers) == 0 {
		return nil, fmt.Errorf("fabric: no installer versions")
	}
	installer := installers[0].Version

	// Fabric serves a self-contained server launcher jar that resolves the rest
	// of its libraries on first run — no installer execution required.
	dlURL := fmt.Sprintf("%s/versions/loader/%s/%s/%s/server/jar", fabricMeta, mcVersion, loader, installer)
	if lg != nil {
		lg.Infof("fabric: downloading launcher (loader %s, installer %s)", loader, installer)
	}
	if _, err := downloadTo(ctx, client, dlURL, dir, "server.jar"); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

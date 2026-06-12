package providers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"mcos/internal/log"
	"mcos/internal/model"
)

const quiltMeta = "https://meta.quiltmc.org/v3"

type quiltProvider struct{}

func (quiltProvider) Software() model.Software { return model.SoftwareQuilt }

type quiltInstallerEntry struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}

func (quiltProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	var installers []quiltInstallerEntry
	if err := getJSON(ctx, client, quiltMeta+"/versions/installer", &installers); err != nil {
		return nil, fmt.Errorf("quilt: installer list: %w", err)
	}
	if len(installers) == 0 || installers[0].URL == "" {
		return nil, fmt.Errorf("quilt: no installer available")
	}
	installerJar, err := downloadTo(ctx, client, installers[0].URL, dir, "quilt-installer.jar")
	if err != nil {
		return nil, err
	}
	defer os.Remove(installerJar)

	if lg != nil {
		lg.Infof("quilt: running installer %s for MC %s", installers[0].Version, mcVersion)
	}
	// Produces quilt-server-launch.jar in dir.
	if _, err := runJava(ctx, javaBin, dir, lg,
		"-jar", "quilt-installer.jar", "install", "server", mcVersion,
		"--download-server", "--install-dir=.",
	); err != nil {
		return nil, err
	}
	launch := "quilt-server-launch.jar"
	if _, err := os.Stat(filepath.Join(dir, launch)); err != nil {
		return nil, fmt.Errorf("quilt: expected %s after install: %w", launch, err)
	}
	return &InstallResult{JarFile: launch, LaunchArgs: []string{"nogui"}}, nil
}

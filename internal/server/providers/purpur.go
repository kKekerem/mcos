package providers

import (
	"context"
	"fmt"
	"net/http"

	"mcos/internal/log"
	"mcos/internal/model"
)

const purpurAPI = "https://api.purpurmc.org/v2/purpur"

type purpurProvider struct{}

func (purpurProvider) Software() model.Software { return model.SoftwarePurpur }

func (purpurProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	// Verify the version exists (the API 404s otherwise) then download latest.
	var meta struct {
		Builds struct {
			Latest string `json:"latest"`
		} `json:"builds"`
	}
	if err := getJSON(ctx, client, fmt.Sprintf("%s/%s", purpurAPI, mcVersion), &meta); err != nil {
		return nil, fmt.Errorf("purpur: version %q: %w", mcVersion, err)
	}
	dlURL := fmt.Sprintf("%s/%s/latest/download", purpurAPI, mcVersion)
	if lg != nil {
		lg.Infof("purpur: downloading %s (build %s)", mcVersion, meta.Builds.Latest)
	}
	if _, err := downloadTo(ctx, client, dlURL, dir, "server.jar"); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

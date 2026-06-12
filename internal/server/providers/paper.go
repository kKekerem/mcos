package providers

import (
	"context"
	"fmt"
	"net/http"

	"mcos/internal/log"
	"mcos/internal/model"
)

const paperAPI = "https://api.papermc.io/v2/projects"

// paperLikeProvider serves PaperMC-family projects (Paper, Folia) which share
// the same v2 API shape.
type paperLikeProvider struct {
	software model.Software
	project  string // "paper" | "folia"
}

func (p paperLikeProvider) Software() model.Software { return p.software }

type paperVersionBuilds struct {
	Builds []int `json:"builds"`
}

type paperBuild struct {
	Downloads struct {
		Application struct {
			Name string `json:"name"`
		} `json:"application"`
	} `json:"downloads"`
}

func (p paperLikeProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	var vb paperVersionBuilds
	url := fmt.Sprintf("%s/%s/versions/%s", paperAPI, p.project, mcVersion)
	if err := getJSON(ctx, client, url, &vb); err != nil {
		return nil, fmt.Errorf("%s: version %q: %w", p.project, mcVersion, err)
	}
	if len(vb.Builds) == 0 {
		return nil, fmt.Errorf("%s: no builds for %q", p.project, mcVersion)
	}
	build := vb.Builds[len(vb.Builds)-1] // latest

	var b paperBuild
	burl := fmt.Sprintf("%s/%s/versions/%s/builds/%d", paperAPI, p.project, mcVersion, build)
	if err := getJSON(ctx, client, burl, &b); err != nil {
		return nil, fmt.Errorf("%s: build %d: %w", p.project, build, err)
	}
	jarName := b.Downloads.Application.Name
	if jarName == "" {
		return nil, fmt.Errorf("%s: build %d has no application jar", p.project, build)
	}
	dlURL := fmt.Sprintf("%s/%s/versions/%s/builds/%d/downloads/%s", paperAPI, p.project, mcVersion, build, jarName)
	if lg != nil {
		lg.Infof("%s: downloading %s (build %d)", p.project, jarName, build)
	}
	if _, err := downloadTo(ctx, client, dlURL, dir, "server.jar"); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

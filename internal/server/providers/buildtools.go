package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mcos/internal/log"
	"mcos/internal/model"
)

const buildToolsURL = "https://hub.spigotmc.org/jenkins/job/BuildTools/lastSuccessfulBuild/artifact/target/BuildTools.jar"

// buildToolsProvider builds Spigot or CraftBukkit from source using the
// upstream BuildTools.jar. This compiles the server (requires git + Java and
// several minutes) because Spigot/CraftBukkit may not be redistributed directly.
type buildToolsProvider struct {
	software model.Software
	target   string // "spigot" | "craftbukkit"
}

func (p buildToolsProvider) Software() model.Software { return p.software }

func (p buildToolsProvider) Install(ctx context.Context, mcVersion, dir, javaBin string, client *http.Client, lg *log.Logger) (*InstallResult, error) {
	buildDir := filepath.Join(dir, ".buildtools")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return nil, err
	}
	if lg != nil {
		lg.Infof("%s: downloading BuildTools", p.software)
	}
	if _, err := downloadTo(ctx, client, buildToolsURL, buildDir, "BuildTools.jar"); err != nil {
		return nil, err
	}
	if lg != nil {
		lg.Infof("%s: compiling %s %s with BuildTools (several minutes)", p.software, p.target, mcVersion)
	}
	if _, err := runJava(ctx, javaBin, buildDir, lg,
		"-jar", "BuildTools.jar", "--rev", mcVersion, "--compile", strings.ToUpper(p.target),
	); err != nil {
		return nil, fmt.Errorf("%s: BuildTools: %w", p.software, err)
	}

	// BuildTools writes <target>-<version>.jar into the build dir.
	produced := findBuiltJar(buildDir, p.target)
	if produced == "" {
		return nil, fmt.Errorf("%s: built jar not found in %s", p.software, buildDir)
	}
	dst := filepath.Join(dir, "server.jar")
	if err := copyFile(produced, dst); err != nil {
		return nil, err
	}
	return &InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, nil
}

func findBuiltJar(dir, target string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, target+"-") && strings.HasSuffix(name, ".jar") {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

package server

import (
	"strings"
	"testing"

	"mcos/internal/server/providers"
)

func TestBuildLaunchArgsJar(t *testing.T) {
	li := &launchInfo{InstallResult: providers.InstallResult{JarFile: "server.jar", LaunchArgs: []string{"nogui"}}, Installed: true}
	args, err := buildLaunchArgs([]string{"-Xmx2048M"}, li)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if got != "-Xmx2048M -jar server.jar nogui" {
		t.Fatalf("jar launch args = %q", got)
	}
}

func TestBuildLaunchArgsArgsFile(t *testing.T) {
	li := &launchInfo{InstallResult: providers.InstallResult{ArgsFile: "libraries/x/unix_args.txt", LaunchArgs: []string{"nogui"}}, Installed: true}
	args, err := buildLaunchArgs([]string{"-Xmx4096M"}, li)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	if got != "-Xmx4096M @libraries/x/unix_args.txt nogui" {
		t.Fatalf("argsfile launch args = %q", got)
	}
}

func TestBuildLaunchArgsEmpty(t *testing.T) {
	if _, err := buildLaunchArgs(nil, &launchInfo{}); err == nil {
		t.Fatal("expected error for empty launch info")
	}
}

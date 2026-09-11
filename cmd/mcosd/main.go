// Command mcosd is the MCOS management daemon. It owns all server, Java, backup,
// cluster, and tunnel state and exposes it over JSON-RPC for the panels and CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"mcos/internal/daemon"
	"mcos/internal/ipc"
	mlog "mcos/internal/log"
)

func defaultDataRoot() string {
	if runtime.GOOS == "linux" {
		return "/var/lib/mcos"
	}
	return filepath.Join(".", "run")
}

func defaultConfigPath(dataRoot string) string {
	if runtime.GOOS == "linux" {
		return "/etc/mcos/config.json"
	}
	return filepath.Join(dataRoot, "config.json")
}

func main() {
	var (
		dataRoot = flag.String("data-root", defaultDataRoot(), "root directory for all MCOS state")
		cfgPath  = flag.String("config", "", "path to config.json (default: <data-root>/config.json on dev, /etc/mcos/config.json on linux)")
		listen   = flag.String("listen", ipc.DefaultEndpoint(), "IPC endpoint (unix:///path or tcp://host:port)")
		logLevel = flag.String("log-level", "info", "log level: debug|info|warn|error")
	)
	flag.Parse()

	if *cfgPath == "" {
		*cfgPath = defaultConfigPath(*dataRoot)
	}

	logDir := filepath.Join(*dataRoot, "log")
	_ = os.MkdirAll(logDir, 0o755)
	logFile, errFile := os.OpenFile(filepath.Join(logDir, "mcosd.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	var outWriter io.Writer = os.Stderr
	if errFile == nil {
		outWriter = io.MultiWriter(os.Stderr, logFile)
		defer logFile.Close()
	}

	lg := mlog.New(outWriter, parseLevel(*logLevel), 2048)
	lg.Infof("mcosd %s starting (data-root=%s config=%s listen=%s)", daemon.Version, *dataRoot, *cfgPath, *listen)

	d, err := daemon.New(*cfgPath, *dataRoot, lg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcosd: init failed: %v\n", err)
		os.Exit(1)
	}

	ln, err := ipc.Listen(*listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcosd: listen %s: %v\n", *listen, err)
		os.Exit(1)
	}
	lg.Infof("mcosd: listening on %s", *listen)

	srv := ipc.NewServer(ln, lg)
	d.Register(srv)

	ctx, cancel := signalContext()
	defer cancel()

	go func() {
		if err := srv.Serve(ctx); err != nil {
			lg.Errorf("mcosd: serve: %v", err)
		}
	}()

	d.Run(ctx)
	lg.Infof("mcosd: stopped")
}

func parseLevel(s string) mlog.Level {
	switch s {
	case "debug":
		return mlog.LevelDebug
	case "warn":
		return mlog.LevelWarn
	case "error":
		return mlog.LevelError
	default:
		return mlog.LevelInfo
	}
}

// signalContext returns a context cancelled on SIGINT/SIGTERM.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}

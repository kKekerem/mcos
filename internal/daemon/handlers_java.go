package daemon

import (
	"context"
	"encoding/json"

	"mcos/internal/ipc"
)

func (d *Daemon) handleJavaList(_ context.Context, _ json.RawMessage) (any, error) {
	rts, err := d.java.List()
	if err != nil {
		return nil, err
	}
	return ipc.JavaListResult{Runtimes: rts}, nil
}

func (d *Daemon) handleJavaResolve(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaResolveParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	major, installed, err := d.java.Resolve(p.MCVersion)
	if err != nil {
		return nil, err
	}
	return ipc.JavaResolveResult{Major: major, Installed: installed}, nil
}

func (d *Daemon) handleJavaInstall(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Major <= 0 {
		return nil, &ipc.Error{Code: ipc.CodeInvalidParams, Message: "major must be > 0"}
	}
	go func() {
		_, _ = d.java.Install(p.Major)
	}()
	return ipc.OKResult{OK: true, Message: "Java indirmesi başlatıldı"}, nil
}

func (d *Daemon) handleJavaRemove(_ context.Context, raw json.RawMessage) (any, error) {
	var p ipc.JavaInstallParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if err := d.java.Remove(p.Major); err != nil {
		return nil, err
	}
	return ipc.OKResult{OK: true}, nil
}

func (d *Daemon) handleJavaDetect(_ context.Context, _ json.RawMessage) (any, error) {
	rts, err := d.java.Detect()
	if err != nil {
		return nil, err
	}
	return ipc.JavaListResult{Runtimes: rts}, nil
}

func (d *Daemon) handleJavaProgress(_ context.Context, _ json.RawMessage) (any, error) {
	return ipc.JavaProgressResult{Progresses: d.java.ProgressMap()}, nil
}

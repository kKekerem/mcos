//go:build !linux

// Package fbvt stub for non-Linux hosts so the repo builds on a dev machine.
package fbvt

import "errors"

// Console is a no-op outside Linux.
type Console struct{}

// Open always fails off Linux: there is no virtual terminal to take over.
func Open(string, string) (*Console, error) {
	return nil, errors.New("sanal terminal yalnizca Linux'ta devralinabilir")
}

func (*Console) Restore() error             { return nil }
func (*Console) Read(p []byte) (int, error) { return 0, errors.New("konsol yok") }
func (*Console) Blank(bool) bool            { return false }

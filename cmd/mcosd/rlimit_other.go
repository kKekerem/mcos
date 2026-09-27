//go:build !linux

package main

func raiseFileLimit() uint64 { return 0 }

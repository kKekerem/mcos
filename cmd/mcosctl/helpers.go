package main

import (
	"encoding/json"
	"fmt"
	"os"

	"mcos/internal/model"
)

// rawStatus aliases the shared status type for local decoding.
type rawStatus = model.SystemStatus

// gib converts bytes to gibibytes for display.
func gib(b uint64) float64 { return float64(b) / (1 << 30) }

// softwareOf coerces a string into a Software value (validated server-side).
func softwareOf(s string) model.Software { return model.Software(s) }

// printJSON pretty-prints any value as JSON to stdout.
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// fail prints an error and exits non-zero.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mcosctl: "+format+"\n", args...)
	os.Exit(1)
}

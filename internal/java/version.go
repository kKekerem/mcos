// Package java manages multiple JDK runtimes and the mapping from a Minecraft
// version to the Java major version it requires. The download/registry/runtime
// binding parts are added in the Java-manager milestone; the version mapping is
// here because the install wizard needs it from the start.
package java

import (
	"strconv"
	"strings"
)

// RequiredJavaMajor returns the Java major version a given Minecraft release
// needs. Rules (vanilla baseline; modloaders follow the same floor):
//
//	<= 1.16.5            -> Java 8
//	1.17.x              -> Java 16 (17 also works)
//	1.18 .. 1.20.4      -> Java 17
//	>= 1.20.5 (incl 1.21)-> Java 21
//
// Unknown/blank versions default to the modern LTS (21).
func RequiredJavaMajor(mcVersion string) int {
	major, minor, patch, ok := parseMC(mcVersion)
	if !ok {
		return 21
	}
	if major != 1 {
		return 21 // future-proof: any non-1.x line uses modern LTS
	}
	switch {
	case minor <= 16:
		return 8
	case minor == 17:
		return 17
	case minor < 20:
		return 17
	case minor == 20:
		if patch >= 5 {
			return 21
		}
		return 17
	default: // minor >= 21
		return 21
	}
}

// parseMC parses "1.20.4" / "1.21" / "1.8.8" into numeric components.
func parseMC(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	// Strip pre-release/snapshot suffixes like "1.21-rc1".
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, false
	}
	if len(parts) >= 3 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return major, minor, patch, true
}

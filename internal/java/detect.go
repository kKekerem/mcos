package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mcos/internal/model"
)

// Detect scans the host for already-installed JDKs and registers any found.
// On the MCOS appliance this lets a manually-dropped JDK be used; during
// development it lets the Java manager be exercised without a large download.
func (m *Manager) Detect() ([]model.JavaRuntime, error) {
	seen := map[string]bool{}
	var found []model.JavaRuntime

	consider := func(javaHome string) {
		javaHome = filepath.Clean(javaHome)
		if javaHome == "" || seen[javaHome] {
			return
		}
		seen[javaHome] = true
		javaBin := filepath.Join(javaHome, "bin", javaExe())
		if _, err := os.Stat(javaBin); err != nil {
			return
		}
		verLine := probeVersion(javaBin)
		major := majorFromVersion(verLine)
		if major == 0 {
			return
		}
		found = append(found, model.JavaRuntime{
			Major: major, Version: verLine, Vendor: "host",
			Path: javaHome, JavaBin: javaBin, InstalledAt: time.Now(),
		})
	}

	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		consider(jh)
	}
	if p, err := exec.LookPath(javaExe()); err == nil {
		// .../<home>/bin/java  -> home
		consider(filepath.Dir(filepath.Dir(p)))
	}
	for _, dir := range candidateDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			base := filepath.Join(dir, e.Name())
			consider(base)
			consider(filepath.Join(base, "Contents", "Home")) // macOS layout
		}
	}

	// Pick the best runtime per major: prefer a HotSpot build (Temurin/Adoptium)
	// over OpenJ9/Semeru, which some server launchers (e.g. Paper's paperclip)
	// do not patch reliably. Never clobber a Temurin install we manage ourselves.
	best := map[int]model.JavaRuntime{}
	for _, rt := range found {
		cur, ok := best[rt.Major]
		if !ok || runtimeScore(rt) > runtimeScore(cur) {
			best[rt.Major] = rt
		}
	}
	for major, rt := range best {
		if existing, ok, _ := m.Get(major); ok && existing.Vendor == "temurin" {
			continue
		}
		if err := m.register(rt); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// runtimeScore ranks a detected runtime for reliability; higher is better.
func runtimeScore(rt model.JavaRuntime) int {
	p := strings.ToLower(rt.Path + " " + rt.Version)
	switch {
	case strings.Contains(p, "semeru"), strings.Contains(p, "openj9"):
		return 0 // OpenJ9 — deprioritize
	case strings.Contains(p, "temurin"), strings.Contains(p, "adoptium"), strings.Contains(p, "hotspot"):
		return 3 // preferred HotSpot
	default:
		return 1
	}
}

// candidateDirs returns platform-specific roots that commonly hold JDKs.
func candidateDirs() []string {
	switch runtime.GOOS {
	case "windows":
		home, _ := os.UserHomeDir()
		return []string{
			`C:\Program Files\Java`,
			`C:\Program Files\Eclipse Adoptium`,
			`C:\Program Files\Microsoft`,
			filepath.Join(home, ".jdks"), // JetBrains/Android Studio
		}
	case "darwin":
		return []string{"/Library/Java/JavaVirtualMachines"}
	default:
		return []string{"/usr/lib/jvm", "/opt/java", "/opt/mcos/java"}
	}
}

var versionRe = regexp.MustCompile(`version "([^"]+)"`)

// majorFromVersion extracts the Java major from a `java -version` line, handling
// both legacy "1.8.0_402" and modern "21.0.5" forms.
func majorFromVersion(line string) int {
	mm := versionRe.FindStringSubmatch(line)
	if mm == nil {
		return 0
	}
	v := mm[1]
	parts := strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '_' || r == '+' || r == '-' })
	if len(parts) == 0 {
		return 0
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if first == 1 && len(parts) >= 2 {
		if second, err := strconv.Atoi(parts[1]); err == nil {
			return second // 1.8 -> 8
		}
	}
	return first
}

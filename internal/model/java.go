package model

import "time"

// JavaRuntime is one installed JDK, recorded in the Java index.
type JavaRuntime struct {
	Major       int       `json:"major"`   // e.g. 8, 17, 21
	Version     string    `json:"version"` // full version string
	Vendor      string    `json:"vendor"`  // e.g. "temurin"
	Path        string    `json:"path"`    // absolute path to JAVA_HOME
	JavaBin     string    `json:"javaBin"` // absolute path to java executable
	InstalledAt time.Time `json:"installedAt"`
}

// JavaIndex is the persisted registry of installed runtimes.
type JavaIndex struct {
	Runtimes []JavaRuntime `json:"runtimes"`
}

// Find returns the runtime for the given major version, or nil.
func (ix *JavaIndex) Find(major int) *JavaRuntime {
	for i := range ix.Runtimes {
		if ix.Runtimes[i].Major == major {
			return &ix.Runtimes[i]
		}
	}
	return nil
}

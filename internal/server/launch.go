package server

import "fmt"

// buildLaunchArgs assembles the full java argument list from the JVM flags and
// the recorded launch artifact (a jar, a Forge-style @args file, or a script).
func buildLaunchArgs(jvmArgs []string, li *launchInfo) ([]string, error) {
	args := append([]string{}, jvmArgs...)
	switch {
	case li.JarFile != "":
		args = append(args, "-jar", li.JarFile)
		args = append(args, li.LaunchArgs...)
	case li.ArgsFile != "":
		args = append(args, "@"+li.ArgsFile)
		args = append(args, li.LaunchArgs...)
	case li.Script != "":
		return nil, fmt.Errorf("script-based launch (%s) not supported yet", li.Script)
	default:
		return nil, fmt.Errorf("no launch artifact recorded for server")
	}
	return args, nil
}

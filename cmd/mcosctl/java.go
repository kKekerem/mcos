package main

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"mcos/internal/ipc"
)

// cmdJava dispatches the `java` subcommands.
func cmdJava(cli *ipc.Client, args []string) {
	if len(args) == 0 {
		fail("usage: mcosctl java <list|resolve|install|remove|detect> ...")
	}
	switch args[0] {
	case "list", "ls":
		javaList(cli)
	case "resolve":
		requireArg(args, 2, "java resolve <mcVersion>")
		javaResolve(cli, args[1])
	case "install":
		requireArg(args, 2, "java install <major>")
		javaInstall(cli, args[1])
	case "remove", "rm":
		requireArg(args, 2, "java remove <major>")
		javaRemove(cli, args[1])
	case "detect":
		javaDetect(cli)
	default:
		fail("unknown java subcommand %q", args[0])
	}
}

func javaList(cli *ipc.Client) {
	var res ipc.JavaListResult
	if err := cli.Call(ipc.MethodJavaList, nil, &res); err != nil {
		fail("java list: %v", err)
	}
	if len(res.Runtimes) == 0 {
		fmt.Println("(no Java runtimes installed)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MAJOR\tVENDOR\tVERSION\tPATH")
	for _, r := range res.Runtimes {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", r.Major, r.Vendor, r.Version, r.Path)
	}
	w.Flush()
}

func javaResolve(cli *ipc.Client, mc string) {
	var res ipc.JavaResolveResult
	if err := cli.Call(ipc.MethodJavaResolve, ipc.JavaResolveParams{MCVersion: mc}, &res); err != nil {
		fail("java resolve: %v", err)
	}
	fmt.Printf("Minecraft %s requires Java %d (installed=%v)\n", mc, res.Major, res.Installed)
}

func javaInstall(cli *ipc.Client, majorStr string) {
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		fail("major must be a number")
	}
	fmt.Printf("installing Java %d (this downloads ~150-200MB)...\n", major)
	var res ipc.JavaRuntimeResult
	if err := cli.Call(ipc.MethodJavaInstall, ipc.JavaInstallParams{Major: major}, &res); err != nil {
		fail("java install: %v", err)
	}
	fmt.Printf("installed Java %d (%s) at %s\n", res.Runtime.Major, res.Runtime.Version, res.Runtime.Path)
}

func javaRemove(cli *ipc.Client, majorStr string) {
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		fail("major must be a number")
	}
	var res ipc.OKResult
	if err := cli.Call(ipc.MethodJavaRemove, ipc.JavaInstallParams{Major: major}, &res); err != nil {
		fail("java remove: %v", err)
	}
	fmt.Println("removed")
}

func javaDetect(cli *ipc.Client) {
	var res ipc.JavaListResult
	if err := cli.Call(ipc.MethodJavaDetect, nil, &res); err != nil {
		fail("java detect: %v", err)
	}
	fmt.Printf("detected %d runtime(s):\n", len(res.Runtimes))
	for _, r := range res.Runtimes {
		fmt.Printf("  Java %d  %s  (%s)\n", r.Major, r.Version, r.Path)
	}
}

// Command mcosctl is a thin CLI client over the mcosd JSON-RPC API, used for
// development, debugging, and automation/scripting from the OS.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"mcos/internal/ipc"
)

func main() {
	connect := flag.String("connect", ipc.DefaultEndpoint(), "mcosd IPC endpoint")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	cli, err := ipc.DialClient(*connect)
	if err != nil {
		fail("connect %s: %v", *connect, err)
	}
	defer cli.Close()

	switch args[0] {
	case "ping":
		cmdPing(cli)
	case "status":
		cmdStatus(cli)
	case "servers", "ls":
		cmdServers(cli)
	case "get":
		requireArg(args, 2, "get <id>")
		cmdGet(cli, args[1])
	case "create":
		cmdCreate(cli, args[1:])
	case "delete", "rm":
		requireArg(args, 2, "delete <id>")
		cmdDelete(cli, args[1])
	case "install":
		requireArg(args, 2, "install <id>")
		cmdServerAction(cli, ipc.MethodServerInstall, args[1])
	case "start":
		requireArg(args, 2, "start <id>")
		cmdServerAction(cli, ipc.MethodServerStart, args[1])
	case "stop":
		requireArg(args, 2, "stop <id>")
		cmdServerAction(cli, ipc.MethodServerStop, args[1])
	case "restart":
		requireArg(args, 2, "restart <id>")
		cmdServerAction(cli, ipc.MethodServerRestart, args[1])
	case "cmd":
		requireArg(args, 3, "cmd <id> <command...>")
		cmdSend(cli, args[1], args[2:])
	case "console":
		requireArg(args, 2, "console <id>")
		cmdConsole(cli, args[1])
	case "config":
		cmdConfig(cli)
	case "java":
		cmdJava(cli, args[1:])
	case "backup":
		requireArg(args, 3, "backup list <serverID>")
		cmdBackupList(cli, args[2])
	case "files":
		requireArg(args, 3, "files list <serverID> [path]")
		path := "."
		if len(args) >= 4 {
			path = args[3]
		}
		cmdFilesList(cli, args[2], path)
	case "worlds":
		requireArg(args, 3, "worlds list <serverID>")
		cmdWorldsList(cli, args[2])
	case "players":
		requireArg(args, 3, "players list <serverID>")
		cmdPlayersList(cli, args[2])
	case "cluster":
		requireArg(args, 2, "cluster peers | tasks")
		switch args[1] {
		case "peers":
			cmdClusterPeers(cli)
		case "tasks":
			cmdClusterTasks(cli)
		default:
			fail("unknown cluster subcommand %q", args[1])
		}
	case "tunnel":
		requireArg(args, 2, "tunnel compress <command> | resolve <code> | start <code> | stop <code> | list")
		switch args[1] {
		case "compress":
			requireArg(args, 3, "tunnel compress <command...>")
			cmdTunnelCompress(cli, strings.Join(args[2:], " "))
		case "resolve":
			requireArg(args, 3, "tunnel resolve <code>")
			cmdTunnelResolve(cli, args[2])
		case "start":
			requireArg(args, 3, "tunnel start <code>")
			cmdTunnelStart(cli, args[2])
		case "stop":
			requireArg(args, 3, "tunnel stop <code>")
			cmdTunnelStop(cli, args[2])
		case "list":
			cmdTunnelList(cli)
		default:
			fail("unknown tunnel subcommand %q", args[1])
		}
	default:
		fail("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `mcosctl — MCOS control client

Usage:
  mcosctl [--connect ENDPOINT] <command> [args]

Commands:
  ping                       check daemon connectivity
  status                     print system status
  servers                    list servers
  get <id>                   show one server
  create --name N --software paper --mc 1.21.1 --ram 2048 [--port P] [--autostart]
  delete <id>                remove a server
  install <id>               download/prepare server software
  start <id>                 start a server
  stop <id>                  stop a server
  restart <id>               restart a server
  cmd <id> <command...>      send a console command
  console <id>               stream the live console (Ctrl-C to exit)
  config                     print global config
  java list                  list installed Java runtimes
  java resolve <mcVersion>   show required Java major + install state
  java install <major>       download+install a Temurin JDK
  java remove <major>        remove an installed JDK
  java detect                scan host for installed JDKs and register them
  backup list <serverID>     list backups for a server
  files list <serverID> [path] list files inside a server's data directory
  worlds list <serverID>     list Minecraft worlds for a server
  players list <serverID>    list online players for a server
  cluster peers              list known cluster peers
  cluster tasks              list local cluster tasks
  tunnel compress <cmd>      compress a wan command into 5-char code
  tunnel resolve <code>      resolve a 5-char code back to command
  tunnel start <code>        start a tunnel by code
  tunnel stop <code>         stop a tunnel by code
  tunnel list                list all tunnels

Default endpoint: %s
`, ipc.DefaultEndpoint())
}

func requireArg(args []string, n int, form string) {
	if len(args) < n {
		fail("usage: mcosctl %s", form)
	}
}

func cmdPing(cli *ipc.Client) {
	var res ipc.PingResult
	if err := cli.Call(ipc.MethodPing, nil, &res); err != nil {
		fail("ping: %v", err)
	}
	fmt.Printf("pong=%v version=%s\n", res.Pong, res.Version)
}

func cmdStatus(cli *ipc.Client) {
	var st rawStatus
	if err := cli.Call(ipc.MethodSystemStatus, nil, &st); err != nil {
		fail("status: %v", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "System:\t%s %s\n", st.SystemName, st.Version)
	fmt.Fprintf(w, "Tier:\t%s\n", st.Tier)
	fmt.Fprintf(w, "CPU:\t%s (%d cores / %d threads) %.0f%%\n", st.CPU.Model, st.CPU.Cores, st.CPU.Threads, st.CPU.UsagePct)
	fmt.Fprintf(w, "Memory:\t%.1f GiB total, %.1f GiB free\n", gib(st.Memory.TotalBytes), gib(st.Memory.AvailableBytes))
	fmt.Fprintf(w, "Local IP:\t%s\tInternet:\t%v\n", st.Net.LocalIP, st.Net.Internet)
	fmt.Fprintf(w, "Servers:\t%d total, %d running\n", st.ServersTotal, st.ServersUp)
	fmt.Fprintf(w, "Java:\t%v\n", st.JavaVersions)
	fmt.Fprintf(w, "WAN:\t%s\tPeers:\t%d\n", st.WAN, st.PeersOnline)
	w.Flush()
}

func cmdServers(cli *ipc.Client) {
	var res ipc.ServerListResult
	if err := cli.Call(ipc.MethodServerList, nil, &res); err != nil {
		fail("servers: %v", err)
	}
	if len(res.Servers) == 0 {
		fmt.Println("(no servers — run the install wizard)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSOFTWARE\tMC\tJAVA\tRAM\tPORT\tSTATE")
	for _, s := range res.Servers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%dMB\t%d\t%s\n",
			s.ID, s.Name, s.Software, s.MCVersion, s.JavaMajor, s.RAMMB, s.Port, s.State)
	}
	w.Flush()
}

func cmdGet(cli *ipc.Client, id string) {
	var res ipc.ServerResult
	if err := cli.Call(ipc.MethodServerGet, ipc.IDParams{ID: id}, &res); err != nil {
		fail("get: %v", err)
	}
	printJSON(res.Server)
}

func cmdCreate(cli *ipc.Client, args []string) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	name := fs.String("name", "", "server name")
	software := fs.String("software", "paper", "server software")
	mc := fs.String("mc", "", "Minecraft version")
	ram := fs.Int("ram", 2048, "RAM in MB")
	port := fs.Int("port", 0, "port (0 = default)")
	autostart := fs.Bool("autostart", false, "start on boot")
	_ = fs.Parse(args)

	p := ipc.ServerCreateParams{
		Name: *name, Software: softwareOf(*software), MCVersion: *mc,
		RAMMB: *ram, Port: *port, Autostart: *autostart,
	}
	var res ipc.ServerResult
	if err := cli.Call(ipc.MethodServerCreate, p, &res); err != nil {
		fail("create: %v", err)
	}
	fmt.Printf("created %s (%s)\n", res.Server.Name, res.Server.ID)
}

func cmdDelete(cli *ipc.Client, id string) {
	var res ipc.OKResult
	if err := cli.Call(ipc.MethodServerDelete, ipc.IDParams{ID: id}, &res); err != nil {
		fail("delete: %v", err)
	}
	fmt.Println("deleted")
}

func cmdConfig(cli *ipc.Client) {
	var res ipc.ConfigResult
	if err := cli.Call(ipc.MethodConfigGet, nil, &res); err != nil {
		fail("config: %v", err)
	}
	printJSON(res.Config)
}

func cmdBackupList(cli *ipc.Client, serverID string) {
	var res ipc.BackupListResult
	if err := cli.Call(ipc.MethodBackupList, ipc.IDParams{ID: serverID}, &res); err != nil {
		fail("backup list: %v", err)
	}
	if len(res.Backups) == 0 {
		fmt.Println("(no backups)")
		return
	}
	for _, b := range res.Backups {
		fmt.Printf("%s  %s  %s  %d bytes\n", b.ID, b.Name, b.CreatedAt.Format("2006-01-02 15:04"), b.SizeBytes)
	}
}

func cmdFilesList(cli *ipc.Client, serverID, path string) {
	var res ipc.FilesListResult
	if err := cli.Call(ipc.MethodFilesList, ipc.FilesListParams{ServerID: serverID, Path: path}, &res); err != nil {
		fail("files list: %v", err)
	}
	if len(res.Entries) == 0 {
		fmt.Println("(no files)")
		return
	}
	for _, e := range res.Entries {
		mark := " "
		if e.IsDir {
			mark = "D"
		}
		fmt.Printf("%s %8d  %s  %s\n", mark, e.Size, e.Mode, e.Name)
	}
}

func cmdWorldsList(cli *ipc.Client, serverID string) {
	var res ipc.WorldsListResult
	if err := cli.Call(ipc.MethodWorldsList, ipc.IDParams{ID: serverID}, &res); err != nil {
		fail("worlds list: %v", err)
	}
	if len(res.Worlds) == 0 {
		fmt.Println("(no worlds)")
		return
	}
	for _, w := range res.Worlds {
		fmt.Printf("%s  %d bytes\n", w.Name, w.SizeBytes)
	}
}

func cmdPlayersList(cli *ipc.Client, serverID string) {
	var res ipc.PlayersListResult
	if err := cli.Call(ipc.MethodPlayersList, ipc.IDParams{ID: serverID}, &res); err != nil {
		fail("players list: %v", err)
	}
	fmt.Printf("Online: %d / %d\n", res.Online, res.Max)
	for _, p := range res.Players {
		fmt.Printf("  • %s\n", p.Name)
	}
}

func cmdClusterPeers(cli *ipc.Client) {
	var res ipc.ClusterPeersResult
	if err := cli.Call(ipc.MethodClusterPeers, nil, &res); err != nil {
		fail("cluster peers: %v", err)
	}
	if len(res.Peers) == 0 {
		fmt.Println("(no peers)")
		return
	}
	for _, p := range res.Peers {
		paired := ""
		if p.Paired {
			paired = " (paired)"
		}
		fmt.Printf("%s  %s  %s  cores=%d  ram=%dMB%s\n", p.ID, p.Name, p.State, p.Cores, p.RAMMB, paired)
	}
}

func cmdClusterTasks(cli *ipc.Client) {
	var res ipc.ClusterTasksResult
	if err := cli.Call(ipc.MethodClusterTasks, nil, &res); err != nil {
		fail("cluster tasks: %v", err)
	}
	if len(res.Tasks) == 0 {
		fmt.Println("(no tasks)")
		return
	}
	for _, t := range res.Tasks {
		fmt.Printf("%s  %s  %s  assigned=%s\n", t.ID, t.Kind, t.State, t.AssignedTo)
	}
}

func cmdTunnelCompress(cli *ipc.Client, command string) {
	var res ipc.TunnelCompressResult
	if err := cli.Call(ipc.MethodTunnelCreate, ipc.TunnelCompressParams{Command: command}, &res); err != nil {
		fail("tunnel compress: %v", err)
	}
	fmt.Printf("5-char code: %s\n", res.Code)
}

func cmdTunnelResolve(cli *ipc.Client, code string) {
	var res ipc.TunnelResolveResult
	if err := cli.Call(ipc.MethodTunnelResolve, ipc.TunnelResolveParams{Code: code}, &res); err != nil {
		fail("tunnel resolve: %v", err)
	}
	fmt.Printf("Command: %s\n", res.Command)
}

func cmdTunnelStart(cli *ipc.Client, code string) {
	var res ipc.OKResult
	if err := cli.Call(ipc.MethodTunnelStart, ipc.TunnelStatusParams{Code: code}, &res); err != nil {
		fail("tunnel start: %v", err)
	}
	fmt.Println("Tunnel started")
}

func cmdTunnelStop(cli *ipc.Client, code string) {
	var res ipc.OKResult
	if err := cli.Call(ipc.MethodTunnelStop, ipc.TunnelStatusParams{Code: code}, &res); err != nil {
		fail("tunnel stop: %v", err)
	}
	fmt.Println("Tunnel stopped")
}

func cmdTunnelList(cli *ipc.Client) {
	var res ipc.TunnelListResult
	if err := cli.Call(ipc.MethodTunnelList, nil, &res); err != nil {
		fail("tunnel list: %v", err)
	}
	if len(res.Tunnels) == 0 {
		fmt.Println("(no tunnels)")
		return
	}
	for _, t := range res.Tunnels {
		state := "stopped"
		if t.Running {
			state = fmt.Sprintf("running pid=%d", t.PID)
		}
		fmt.Printf("%s  %s  %s\n", t.Code, state, t.Command)
	}
}

package main

import (
	"fmt"
	"strings"
	"time"

	"mcos/internal/ipc"
)

// cmdServerAction invokes a simple id-only server action (install/start/stop/restart).
func cmdServerAction(cli *ipc.Client, method, id string) {
	var res ipc.OKResult
	if err := cli.Call(method, ipc.IDParams{ID: id}, &res); err != nil {
		fail("%s: %v", method, err)
	}
	msg := res.Message
	if msg == "" {
		msg = "ok"
	}
	fmt.Printf("%s: %s\n", method, msg)
}

// cmdSend sends a console command to a server.
func cmdSend(cli *ipc.Client, id string, words []string) {
	command := strings.Join(words, " ")
	var res ipc.OKResult
	if err := cli.Call(ipc.MethodServerCommand, ipc.CommandParams{ID: id, Command: command}, &res); err != nil {
		fail("cmd: %v", err)
	}
	fmt.Printf("sent: %s\n", command)
}

// cmdConsole tails the live console, polling for new lines.
func cmdConsole(cli *ipc.Client, id string) {
	var cursor int64
	fmt.Printf("--- streaming console for %s (Ctrl-C to exit) ---\n", id)
	for {
		var res ipc.ConsoleResult
		if err := cli.Call(ipc.MethodServerConsole, ipc.ConsoleParams{ID: id, Cursor: cursor}, &res); err != nil {
			fail("console: %v", err)
		}
		for _, line := range res.Lines {
			fmt.Println(line.Text)
		}
		cursor = res.Cursor
		time.Sleep(500 * time.Millisecond)
	}
}

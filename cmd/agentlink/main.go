package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/team/agentlink/pkg/cli"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "send":
		cmdSend(os.Args[2:])
	case "pull":
		cmdPull(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`agentlink - Agent communication tool

Usage:
  agentlink init --server <url> --password <pw> [--device <name>] [./path]
  agentlink send <target> <content>
  agentlink pull [--all]
`)
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	server := fs.String("server", "", "API server URL")
	password := fs.String("password", "", "Registration password")
	device := fs.String("device", "", "Device name (default: hostname)")
	fs.Parse(args)

	if *server == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "init: --server and --password are required")
		os.Exit(1)
	}

	path := fs.Arg(0)
	if path == "" {
		path = "./agent_team"
	}

	opts := &cli.InitOptions{
		Server:   *server,
		Password: *password,
		Device:   *device,
		Path:     path,
	}

	if err := cli.RunInit(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdSend(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: agentlink send <target> <content>")
		os.Exit(1)
	}
	target := args[0]
	content := strings.Join(args[1:], " ")

	if err := cli.RunSend(target, content); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdPull(args []string) {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	all := fs.Bool("all", false, "Pull all available messages (max 10)")
	fs.Parse(args)

	if err := cli.RunPull(*all); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

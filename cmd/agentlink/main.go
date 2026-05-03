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
	case "task":
		cmdTask(os.Args[2:])
	case "ping":
		cmdPing()
	case "list":
		cmdList(os.Args[2:])
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
  agentlink ping
  agentlink list [--all]
  agentlink task send <target> <task_id> <content>
  agentlink task result <task_id> <status> <result>
  agentlink task resume <task_id> <guidance>
  agentlink task cancel <task_id>
  agentlink task status <task_id>
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

func cmdPing() {
	if err := cli.RunPing(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	all := fs.Bool("all", false, "Show all devices")
	fs.Parse(args)

	if err := cli.RunList(*all); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTask(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task <subcommand> [args]")
		os.Exit(1)
	}
	switch args[0] {
	case "send":
		cmdTaskSend(args[1:])
	case "result":
		cmdTaskResult(args[1:])
	case "resume":
		cmdTaskResume(args[1:])
	case "cancel":
		cmdTaskCancel(args[1:])
	case "status":
		cmdTaskStatus(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdTaskSend(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task send <target> <task_id> <content>")
		os.Exit(1)
	}
	target := args[0]
	taskID := args[1]
	content := strings.Join(args[2:], " ")

	if err := cli.RunTaskSend(target, taskID, content); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTaskResult(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task result <task_id> <status> <result>")
		os.Exit(1)
	}
	taskID := args[0]
	status := args[1]
	result := strings.Join(args[2:], " ")

	if err := cli.RunTaskResult(taskID, status, result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTaskResume(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task resume <task_id> <guidance>")
		os.Exit(1)
	}
	taskID := args[0]
	guidance := strings.Join(args[1:], " ")

	if err := cli.RunTaskResume(taskID, guidance); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTaskCancel(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task cancel <task_id>")
		os.Exit(1)
	}
	taskID := args[0]

	if err := cli.RunTaskCancel(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTaskStatus(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task status <task_id>")
		os.Exit(1)
	}
	taskID := args[0]

	if err := cli.RunTaskStatus(taskID); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/team/agentlink/pkg/adapter"
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
	case "poll":
		cmdPoll(os.Args[2:])
	case "ping":
		cmdPing()
	case "list":
		cmdList(os.Args[2:])
	case "session":
		cmdSession(os.Args[2:])
	case "attach":
		cmdAttach(os.Args[2:])
	case "uninstall":
		cmdUninstall()
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
  agentlink poll
  agentlink task send <target> <task_id> <content>
  agentlink task result <task_id> <status> <result>
  agentlink task resume <task_id> <guidance>
  agentlink task cancel <task_id>
  agentlink task status <task_id>
  agentlink task list
  agentlink session add|remove <name>
  agentlink attach <session>
  agentlink uninstall
`)
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	server := fs.String("server", "", "API server URL")
	password := fs.String("password", "", "Registration password")
	device := fs.String("device", "", "Device name (default: hostname)")
	agent := fs.String("agent", "claude", "Agent type (default: claude)")
	noPoll := fs.Bool("no-poll", false, "Disable auto-polling (default: false)")
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
		Agent:    *agent,
	}

	if err := cli.RunInit(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Create background tmux sessions
	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	selfExe, _ := os.Executable()
	launcher := adapter.NewLauncher(*agent)
	for _, session := range []string{"main", "worker"} {
		exec.Command("tmux", "kill-session", "-t", session).Run()
		exec.Command("tmux", "kill-session", "-t", session+"-poller").Run()
		dir := filepath.Join(absPath, session)
		name, args := launcher.Command()
		cmdArgs := append([]string{"new-session", "-d", "-s", session, "-c", dir, name}, args...)
		exec.Command("tmux", cmdArgs...).Run()
			if !*noPoll {
		exec.Command("tmux", "new-session", "-d", "-s", session+"-poller", "-c", dir, selfExe, "poll").Run()
			}
	}

		if *noPoll {
			fmt.Println("✓ tmux sessions created: main, worker")
			fmt.Println("  Auto-polling disabled (use agentlink poll to start manually)")
		} else {
			fmt.Println("✓ tmux sessions created: main, worker")
			fmt.Println("✓ poller sessions created: main-poller, worker-poller")
		}
	fmt.Println()
	fmt.Println("Attaching to main session...")

	attach := exec.Command("tmux", "attach", "-t", "main")
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr
	if err := attach.Run(); err != nil {
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

func cmdPoll(args []string) {
	if err := cli.RunPoll(); err != nil {
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
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: agentlink task send <target> [<task_id>] <content>")
		fmt.Fprintln(os.Stderr, "  with task_id: agentlink task send worker my-id \"do something\"")
		fmt.Fprintln(os.Stderr, "  without:      agentlink task send worker \"do something\"")
		os.Exit(1)
	}
	target := args[0]
	var taskID string
	var content string
	if len(args) == 2 {
		// 2-arg form: target + content, server generates task_id
		content = args[1]
	} else {
		// 3+ arg form: target + task_id + content
		taskID = args[1]
		content = strings.Join(args[2:], " ")
	}

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

func cmdSession(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: agentlink session add|remove <name>")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		cmdSessionAdd(args[1:])
	case "remove":
		cmdSessionRemove(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown session subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdSessionAdd(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink session add <name>")
		os.Exit(1)
	}
	name := args[0]
	if err := cli.RunSessionAdd(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdSessionRemove(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink session remove <name>")
		os.Exit(1)
	}
	name := args[0]
	if err := cli.RunSessionRemove(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdAttach(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: agentlink attach <session>")
		os.Exit(1)
	}
	session := args[0]
	if err := cli.RunAttach(session); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdUninstall() {
	if err := cli.RunUninstall(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func cmdTaskList() {
	if err := cli.RunTaskList(); err != nil {
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

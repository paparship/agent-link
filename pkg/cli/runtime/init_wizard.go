package rt

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// IsInteractive reports whether stdin is connected to a terminal. The wizard
// only runs when it is; piped/redirected stdin (CI, scripts) must not hang
// waiting for input.
func IsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// PromptInitOptions fills in any empty required fields (Server, Password,
// Device) on opts by interactively prompting the user. Each field is validated
// and re-asked on failure rather than aborting the whole command. Only call
// this when IsInteractive() is true.
func PromptInitOptions(opts *InitOptions) error {
	fmt.Println("交互式初始化 agentlink(直接回车采用 [] 中默认值)")
	fmt.Println()

	// ① Server URL — validated by probing GET /health.
	if opts.Server == "" {
		def := readExistingServer()
		ok := false
		for attempt := 0; attempt < 3; attempt++ {
			val := normalizeServer(promptLine("Server URL", def))
			if val == "" {
				fmt.Println("  server 不能为空")
				continue
			}
			fmt.Printf("  正在探测 %s/health ...\n", val)
			if err := probeHealth(val); err != nil {
				fmt.Printf("  连接失败: %v\n", err)
				def = val
				continue
			}
			fmt.Println("  ✓ 已连接")
			opts.Server = val
			ok = true
			break
		}
		if !ok {
			return fmt.Errorf("无法连接到 server(重试 3 次后放弃)")
		}
	} else {
		opts.Server = normalizeServer(opts.Server)
	}

	// ② Password — hidden input. Correctness is checked at registration time
	// (RunInit re-asks on a 401 when opts.Interactive is set).
	if opts.Password == "" {
		for attempt := 0; attempt < 3; attempt++ {
			pw := promptSecret("注册密码")
			if pw != "" {
				opts.Password = pw
				break
			}
			fmt.Println("  密码不能为空")
		}
		if opts.Password == "" {
			return fmt.Errorf("未输入密码(重试 3 次后放弃)")
		}
	}

	// ③ Device name — defaults to hostname.
	if opts.Device == "" {
		host, _ := os.Hostname()
		opts.Device = promptLine("设备名", host)
		if opts.Device == "" {
			opts.Device = host
		}
	}

	return nil
}

// ConfirmInitSummary prints the resolved settings and asks for a y/N
// confirmation before anything is created. Returns true to proceed.
func ConfirmInitSummary(opts *InitOptions) bool {
	poll := "开启"
	if opts.NoPoll {
		poll = "关闭"
	}
	fmt.Println()
	fmt.Println("即将初始化:")
	fmt.Printf("  Server : %s\n", opts.Server)
	fmt.Printf("  Device : %s\n", opts.Device)
	fmt.Printf("  Path   : %s\n", opts.Path)
	fmt.Printf("  Poll   : %s\n", poll)
	if opts.Force {
		fmt.Println("  注意   : --force 已启用,已存在的目录将被覆盖")
	}
	fmt.Println()
	return promptConfirm("确认继续?")
}

// normalizeServer trims whitespace, adds an http:// scheme when missing, and
// strips a trailing slash so the URL is safe to append paths to.
func normalizeServer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}

// probeHealth checks that server responds 200 on /health with {"ok":true}.
func probeHealth(server string) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(server + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	var doc struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err == nil && !doc.OK {
		return fmt.Errorf("server reports not ok")
	}
	return nil
}

// readExistingServer returns the server URL from a prior ~/.agentlink/config.toml
// (used as the default when re-running init), or "" if absent.
func readExistingServer() string {
	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".agentlink", "config.toml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "server") {
			i := strings.Index(line, "\"")
			j := strings.LastIndex(line, "\"")
			if i >= 0 && j > i {
				return line[i+1 : j]
			}
		}
	}
	return ""
}

// promptAgentChoice asks which agent type a session should use, listing the
// supported catalog annotated with local availability. Enter selects the
// default (the first installed agent, else the first supported). The chosen
// type is permanent for the session (issue 35).
func promptAgentChoice(session string, supported, avail []string) string {
	installed := map[string]bool{}
	for _, a := range avail {
		installed[a] = true
	}
	def := supported[0]
	for _, a := range supported {
		if installed[a] {
			def = a
			break
		}
	}
	for {
		fmt.Printf("session %q 用哪个 agent?(创建后不可更改)\n", session)
		for i, a := range supported {
			mark := "未检测到"
			if installed[a] {
				mark = "已安装"
			}
			suffix := ""
			if a == def {
				suffix = "  (默认)"
			}
			fmt.Printf("  %d. %-8s (%s)%s\n", i+1, a, mark, suffix)
		}
		choice := promptLine("选择(序号或名称)", def)
		if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(supported) {
			return supported[n-1]
		}
		for _, a := range supported {
			if choice == a {
				return a
			}
		}
		fmt.Printf("  无效选择:%q\n", choice)
	}
}

// promptLine prints "label [def]: ", reads one line, and returns the trimmed
// input or def when the user just presses Enter.
func promptLine(label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line := strings.TrimSpace(readLine())
	if line == "" {
		return def
	}
	return line
}

// promptSecret reads a line with terminal echo disabled via stty, so the typed
// password is not shown. Falls back to visible input if stty is unavailable.
func promptSecret(label string) string {
	fmt.Printf("%s: ", label)
	setEcho(false)
	line := strings.TrimSpace(readLine())
	setEcho(true)
	fmt.Println()
	return line
}

// promptConfirm asks a y/N question, defaulting to No.
func promptConfirm(label string) bool {
	fmt.Printf("%s [y/N]: ", label)
	line := strings.ToLower(strings.TrimSpace(readLine()))
	return line == "y" || line == "yes"
}

// setEcho toggles terminal echo using stty, operating on the controlling
// terminal (os.Stdin). Errors are ignored so a missing stty degrades to
// visible input rather than breaking the prompt.
func setEcho(on bool) {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	c := exec.Command("stty", arg)
	c.Stdin = os.Stdin
	_ = c.Run()
}

// readLine reads a single line from stdin one byte at a time. It avoids
// bufio's read-ahead so that a following promptSecret (which toggles echo)
// does not lose buffered bytes.
func readLine() string {
	var b []byte
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			b = append(b, buf[0])
		}
		if err != nil {
			break
		}
	}
	return stripControl(strings.TrimRight(string(b), "\r"))
}

// stripControl removes ANSI escape sequences and other control characters from
// a line. Terminals with "bracketed paste" enabled wrap pasted text in
// ESC[200~ ... ESC[201~; since the wizard reads raw input it must strip those
// markers (and any stray CSI sequences / control bytes) so a pasted value like
// a server URL comes through clean.
func stripControl(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b { // ESC — skip a following CSI sequence (ESC [ ... final)
			if i+1 < len(runes) && runes[i+1] == '[' {
				i += 2
				for i < len(runes) && !(runes[i] >= 0x40 && runes[i] <= 0x7e) {
					i++
				}
			}
			continue
		}
		if r < 0x20 || r == 0x7f { // drop other control chars
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

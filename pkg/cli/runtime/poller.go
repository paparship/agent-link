package rt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/team/agentlink/pkg/adapter"
	api "github.com/team/agentlink/pkg/cli/net"
)

// Poller polls the inbox and injects messages into the agent via tmux.
type Poller struct {
	Session string
	Server  string
	APIKey  string

	// IdleDetector checks whether the agent pane is ready for input.
	IdleDetector adapter.IdleDetector

	// Polling interval. Default 5s.
	Interval time.Duration

	// Log output. Defaults to io.Discard.
	Stdout io.Writer

	// Cancellation. When nil, context.Background() is used.
	Ctx context.Context

	// lastInjectedID is the id of the message we last injected successfully.
	// Used to skip a redelivered message whose ack didn't land (issue 37),
	// so a lost ack causes a re-ack, not a duplicate injection.
	lastInjectedID string

	// Overridable for testing.
	capturePane func(session string) (string, error)
	sendKeys    func(session, text string) error
	httpDo      func(req *http.Request) (*http.Response, error)
}

func (p *Poller) Run() error {
	p.initDefaults()
	lastHeartbeat := time.Now()
	for {
		if err := p.ctx().Err(); err != nil {
			return err
		}

		// Heartbeat every ~12 iterations (60s with 5s interval)
		if time.Since(lastHeartbeat) > 60*time.Second {
			lastHeartbeat = time.Now()
			p.heartbeat()
		}

		msg, err := p.pullOne()
		if err != nil {
			fmt.Fprintf(p.Stdout, "poll error: %s\n", err)
			p.sleep(p.Interval)
			continue
		}

		if msg != nil {
			// Dedup: a message we already injected, redelivered because its ack
			// didn't land last time. Re-ack, don't re-inject (issue 37).
			dup := msg.ID != "" && msg.ID == p.lastInjectedID
			if dup {
				p.ack(msg.ID)
			} else {
				fmt.Fprintf(p.Stdout, "message from %s:%s\n", msg.FromDevice, msg.FromSession)
				if msg.Interrupt {
					fmt.Fprintf(p.Stdout, "interrupt: sending Ctrl+C\n")
					exec.Command("tmux", "send-keys", "-t", "="+p.Session, "Escape").Run()
					time.Sleep(3 * time.Second)
				} else {
					p.waitForIdle()
				}

				pane, err := p.capturePane(p.Session)
				if err == nil && !p.IdleDetector.IsBusy(pane) && p.IdleDetector.IsPromptEmpty(pane) {
					injectContent := msg.Content
					if msg.Type == "msg" {
						prefix := fmt.Sprintf("[message from %s:%s] ", msg.FromDevice, msg.FromSession)
						injectContent = prefix + msg.Content
					} else if msg.Type == "task" {
						injectContent = fmt.Sprintf(
							"[task %s from %s:%s]\n%s\nWhen done: agentlink task result %s completed \"<result>\"\nTo suspend: agentlink task result %s suspended \"<reason>\"",
							msg.TaskID, msg.FromDevice, msg.FromSession,
							msg.Content,
							msg.TaskID, msg.TaskID,
						)
					}
					fmt.Fprintf(p.Stdout, "inject: %s\n", injectContent)
					if err := p.sendKeys(p.Session, injectContent); err != nil {
						// Not acked → the message stays reserved on the server and
						// is redelivered next tick, instead of being lost (issue 37).
						fmt.Fprintf(p.Stdout, "send-keys error: %s (will retry)\n", err)
					} else {
						// Injected → confirm delivery so the server drops it.
						p.lastInjectedID = msg.ID
						p.ack(msg.ID)
					}
				}
				// If not injected (agent busy / dead / capture failed): no ack,
				// so the reserved message is redelivered on a later tick.
			}
		}

		pane, _ := p.capturePane(p.Session)
		if pane != "" && (strings.Contains(pane, "Do you trust") || strings.Contains(pane, "trust this folder")) {
			fmt.Fprintf(p.Stdout, "auto-accept trust prompt\n")
			p.sendKeys(p.Session, "1")
		}

		p.sleep(p.Interval)
	}
}

func (p *Poller) heartbeat() {
	req, err := http.NewRequestWithContext(p.ctx(), "POST", p.Server+"/agents/heartbeat", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := p.httpDo(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// -- internal --

type pollerInboxItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	FromDevice  string `json:"from_device"`
	FromSession string `json:"from_session"`
	TaskID      string `json:"task_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Interrupt   bool   `json:"interrupt,omitempty"`
}

type pollerPullResponse struct {
	Items []pollerInboxItem `json:"items"`
}

func (p *Poller) pullOne() (*pollerInboxItem, error) {
	url := fmt.Sprintf("%s/inbox/pull?session=%s&limit=1&reserve=1", p.Server, p.Session)
	req, err := http.NewRequestWithContext(p.ctx(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.httpDo(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("server %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("server %d", resp.StatusCode)
	}

	var pr pollerPullResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	if len(pr.Items) == 0 {
		return nil, nil
	}
	return &pr.Items[0], nil
}

// ack confirms a reserved message was injected, so the server drops it from the
// processing slot. Best-effort: a failed ack just means the message is
// redelivered and de-duplicated via lastInjectedID (issue 37).
func (p *Poller) ack(id string) {
	if id == "" {
		return
	}
	body, _ := json.Marshal(map[string]string{"session": p.Session, "id": id})
	req, err := http.NewRequestWithContext(p.ctx(), "POST", p.Server+"/inbox/ack", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpDo(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (p *Poller) waitForIdle() {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if p.ctx().Err() != nil {
			return
		}
		pane, err := p.capturePane(p.Session)
		if err != nil {
			continue
		}
		if !p.IdleDetector.IsBusy(pane) && p.IdleDetector.IsPromptEmpty(pane) {
			return
		}
		if strings.Contains(pane, "Do you trust") || strings.Contains(pane, "trust this folder") {
			fmt.Fprintf(p.Stdout, "auto-accept trust prompt\n")
			p.sendKeys(p.Session, "1")
		}
		select {
		case <-p.ctx().Done():
			return
		case <-time.After(time.Second):
		}
	}
	fmt.Fprintf(p.Stdout, "waitForIdle timeout after 5m\n")
}

func (p *Poller) sleep(d time.Duration) {
	select {
	case <-p.ctx().Done():
	case <-time.After(d):
	}
}

func (p *Poller) ctx() context.Context {
	if p.Ctx != nil {
		return p.Ctx
	}
	return context.Background()
}

func (p *Poller) initDefaults() {
	if p.Stdout == nil {
		p.Stdout = io.Discard
	}
	if p.Interval <= 0 {
		p.Interval = 5 * time.Second
	}
	if p.capturePane == nil {
		p.capturePane = tmuxCapturePane
	}
	if p.sendKeys == nil {
		p.sendKeys = tmuxSendKeys
	}
	if p.httpDo == nil {
		p.httpDo = http.DefaultClient.Do
	}
}

// -- tmux helpers (package-level for test override) --

var tmuxCapturePane = func(session string) (string, error) {
	var stdout bytes.Buffer
	// "=session" forces an exact match: if the agent session is gone, this must
	// fail rather than prefix-match "<session>-poller" (the poller's own pane)
	// and make the poller capture/inject into itself (issue 32).
	cmd := exec.Command("tmux", "capture-pane", "-p", "-t", "="+session)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

var tmuxSendKeys = func(session, text string) error {
	cmd1 := exec.Command("tmux", "send-keys", "-l", "-t", "="+session, text)
	if err := cmd1.Run(); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	cmd2 := exec.Command("tmux", "send-keys", "-t", "="+session, "Enter")
	return cmd2.Run()
}

// -- entry point --

func RunPoll() error {
	cfg, creds, err := api.LoadAuth()
	if err != nil {
		return err
	}

	if !cfg.Poll.Enabled {
		fmt.Println("Auto-polling is disabled in config")
		return nil
	}

	session, err := api.FindCurrentSession()
	if err != nil {
		return err
	}

	interval := cfg.Poll.Interval
	if interval <= 0 {
		interval = api.DefaultPollInterval
	}

	// Detector depends on this session's own agent type (issue 35), falling
	// back to the device default for legacy sessions without a recorded type.
	agent := api.ReadSessionAgent(filepath.Join(cfg.BaseDir, session))
	if agent == "" {
		agent = cfg.Agent
	}
	detector := adapter.NewDetector(agent)
	if detector == nil {
		return fmt.Errorf("unknown agent type %q for session %q", agent, session)
	}

	p := &Poller{
		Session:      session,
		Server:       cfg.Server,
		APIKey:       creds.APIKey,
		Interval:     time.Duration(interval) * time.Second,
		Stdout:       os.Stdout,
		IdleDetector: detector,
	}
	return p.Run()
}

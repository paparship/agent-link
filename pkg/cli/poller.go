package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/team/agentlink/pkg/adapter"
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

	// Overridable for testing.
	capturePane func(session string) (string, error)
	sendKeys    func(session, text string) error
	httpDo      func(req *http.Request) (*http.Response, error)
}

func (p *Poller) Run() error {
	p.initDefaults()
	for {
		if err := p.ctx().Err(); err != nil {
			return err
		}

		msg, err := p.pullOne()
		if err != nil {
			fmt.Fprintf(p.Stdout, "poll error: %s\n", err)
			p.sleep(p.Interval)
			continue
		}

		if msg != nil {
			fmt.Fprintf(p.Stdout, "message from %s:%s\n", msg.FromDevice, msg.FromSession)
			p.waitForIdle()

			pane, err := p.capturePane(p.Session)
			if err == nil && !p.IdleDetector.IsBusy(pane) && p.IdleDetector.IsPromptEmpty(pane) {
				fmt.Fprintf(p.Stdout, "inject: %s\n", msg.Content)
				if err := p.sendKeys(p.Session, msg.Content); err != nil {
					fmt.Fprintf(p.Stdout, "send-keys error: %s\n", err)
				}
			}
		}

		p.sleep(p.Interval)
	}
}

// -- internal --

type pollerInboxItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	FromDevice  string `json:"from_device"`
	FromSession string `json:"from_session"`
	TaskID      string `json:"task_id,omitempty"`
}

type pollerPullResponse struct {
	Items []pollerInboxItem `json:"items"`
}

func (p *Poller) pullOne() (*pollerInboxItem, error) {
	url := fmt.Sprintf("%s/inbox/pull?session=%s&limit=1", p.Server, p.Session)
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
		var e struct{ Error string `json:"error"` }
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

func (p *Poller) waitForIdle() {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if p.ctx().Err() != nil {
			return
		}
		pane, err := p.capturePane(p.Session)
		if err == nil && !p.IdleDetector.IsBusy(pane) && p.IdleDetector.IsPromptEmpty(pane) {
			return
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
	cmd := exec.Command("tmux", "capture-pane", "-p", "-t", session)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

var tmuxSendKeys = func(session, text string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", session, text, "Enter")
	return cmd.Run()
}

// -- entry point --

func RunPoll() error {
	cfg, creds, err := loadAuth()
	if err != nil {
		return err
	}

	if !cfg.Poll.Enabled {
		fmt.Println("Auto-polling is disabled in config")
		return nil
	}

	session, err := findCurrentSession()
	if err != nil {
		return err
	}

	interval := cfg.Poll.Interval
	if interval <= 0 {
		interval = 5
	}

	p := &Poller{
		Session:  session,
		Server:   cfg.Server,
		APIKey:   creds.APIKey,
		Interval: time.Duration(interval) * time.Second,
		Stdout:   os.Stdout,
		IdleDetector: adapter.NewDetector(cfg.Agent),
	}
	return p.Run()
}

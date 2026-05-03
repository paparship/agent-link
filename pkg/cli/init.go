package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type InitOptions struct {
	Server   string
	Password string
	Device   string
	Path     string
}

type registerRequest struct {
	Device           string   `json:"device"`
	Sessions         []string `json:"sessions"`
	RegisterPassword string   `json:"register_password"`
}

type registerResponse struct {
	APIKey       string   `json:"api_key"`
	Device       string   `json:"device"`
	Sessions     []string `json:"sessions"`
	RegisteredAt string   `json:"registered_at"`
}

func RunInit(opts *InitOptions) error {
	// Resolve device name
	device := opts.Device
	if device == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("cannot determine hostname: %w", err)
		}
		device = hostname
	}

	// Pre-check prerequisites
	if err := checkPrereqs(); err != nil {
		return err
	}

	// Resolve and validate target directory
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return fmt.Errorf("cannot resolve path %q: %w", opts.Path, err)
	}

	if _, err := os.Stat(absPath); err == nil {
		return fmt.Errorf("directory %q already exists", absPath)
	}

	// Create directories
	agentlinkDir := filepath.Join(os.Getenv("HOME"), ".agentlink")
	if err := os.MkdirAll(agentlinkDir, 0755); err != nil {
		return fmt.Errorf("cannot create %s: %w", agentlinkDir, err)
	}

	teamDirs := []string{
		filepath.Join(absPath, "main"),
		filepath.Join(absPath, "worker"),
	}
	for _, d := range teamDirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
	}

	// Write config.toml
	configPath := filepath.Join(agentlinkDir, "config.toml")
	if err := writeConfigTOML(configPath, opts.Server, device, absPath); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}

	// Call register API
	regResp, err := registerDevice(opts.Server, device, opts.Password)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Write credentials.json
	credPath := filepath.Join(agentlinkDir, "credentials.json")
	cred := map[string]string{
		"api_key":       regResp.APIKey,
		"registered_at": regResp.RegisteredAt,
	}
	credData, _ := json.MarshalIndent(cred, "", "  ")
	if err := os.WriteFile(credPath, credData, 0600); err != nil {
		return fmt.Errorf("cannot write credentials: %w", err)
	}

	// Write .agentlink.toml for each session
	for _, session := range regResp.Sessions {
		sessionDir := filepath.Join(absPath, session)
		tomlPath := filepath.Join(sessionDir, ".agentlink.toml")
		if err := writeSessionTOML(tomlPath, session, device); err != nil {
			return fmt.Errorf("cannot write %s: %w", tomlPath, err)
		}
	}

	// Print success
	fmt.Printf("✓ Agent team initialized at %s\n", absPath)
	fmt.Printf("✓ Device %q registered (sessions: %s)\n", device, strings.Join(regResp.Sessions, ", "))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  agentlink attach worker    # switch to worker session")

	return nil
}

func checkPrereqs() error {
	var missing []string
	for _, cmd := range []string{"tmux", "claude"} {
		if _, err := exec.LookPath(cmd); err != nil {
			missing = append(missing, cmd)
		}
	}
	if len(missing) > 0 {
		msg := "require " + strings.Join(missing, " and ") + " to be installed"
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func writeConfigTOML(path, server, device, baseDir string) error {
	content := fmt.Sprintf(`server = %q
device = %q
base_dir = %q
`, server, device, baseDir)
	return os.WriteFile(path, []byte(content), 0600)
}

func writeSessionTOML(path, session, device string) error {
	content := fmt.Sprintf(`session = %q
device = %q
`, session, device)
	return os.WriteFile(path, []byte(content), 0600)
}

func registerDevice(server, device, password string) (*registerResponse, error) {
	body := registerRequest{
		Device:           device,
		Sessions:         []string{"main", "worker"},
		RegisterPassword: password,
	}
	data, _ := json.Marshal(body)

	url := server + "/agents/register"
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot connect to server %s: %w", server, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, errResp.Error)
		}
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var regResp registerResponse
	if err := json.Unmarshal(respBody, &regResp); err != nil {
		return nil, fmt.Errorf("invalid server response: %w", err)
	}
	return &regResp, nil
}


package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AgentConfig struct {
	Server  string
	Device  string
	BaseDir string
	Agent   string
}

type AgentCredentials struct {
	APIKey string `json:"api_key"`
}

func loadConfig() (*AgentConfig, error) {
	path := filepath.Join(os.Getenv("HOME"), ".agentlink", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config file not found at %s", path)
	}

	cfg := &AgentConfig{
		Server:  readTOML(string(data), "server"),
		Device:  readTOML(string(data), "device"),
		BaseDir: readTOML(string(data), "base_dir"),
		Agent:   readTOML(string(data), "agent"),
	}
	if cfg.Server == "" || cfg.Device == "" {
		return nil, fmt.Errorf("invalid config file at %s: missing server or device", path)
	}
	if cfg.Agent == "" {
		cfg.Agent = "claude"
	}
	return cfg, nil
}

func loadCredentials() (*AgentCredentials, error) {
	path := filepath.Join(os.Getenv("HOME"), ".agentlink", "credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credentials file not found at %s", path)
	}

	var creds AgentCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("invalid credentials file at %s: %w", path, err)
	}
	if creds.APIKey == "" {
		return nil, fmt.Errorf("credentials file at %s is missing api_key", path)
	}
	return &creds, nil
}

func findCurrentSession() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		path := filepath.Join(dir, ".agentlink.toml")
		if data, err := os.ReadFile(path); err == nil {
			session := readTOML(string(data), "session")
			if session != "" {
				return session, nil
			}
			return "", fmt.Errorf("session key not found in %s", path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf(".agentlink.toml not found from %s upward", cwd)
}

func readTOML(content, key string) string {
	prefix := key + " = "
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.Trim(val, `"`)
			return val
		}
	}
	return ""
}

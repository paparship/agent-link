package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const DefaultPollInterval = 5

type PollConfig struct {
	Enabled  bool
	Interval int
}

type AgentConfig struct {
	Server   string
	Device   string
	BaseDir  string
	Agent    string
	Poll     PollConfig
	Sessions map[string]string
}

type AgentCredentials struct {
	APIKey string `json:"api_key"`
}

func LoadConfig() (*AgentConfig, error) {
	path := filepath.Join(os.Getenv("HOME"), ".agentlink", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config file not found at %s; run 'agentlink init' first", path)
	}

	cfg := &AgentConfig{
		Server:  ReadTOML(string(data), "server"),
		Device:  ReadTOML(string(data), "device"),
		BaseDir: ReadTOML(string(data), "base_dir"),
		Agent:   ReadTOML(string(data), "agent"),
		Poll: PollConfig{
			Enabled:  ReadTOMLBool(string(data), "poll.enabled", true),
			Interval: ReadTOMLInt(string(data), "poll.interval", 5),
		},
		Sessions: ReadTOMLSection(string(data), "sessions"),
	}
	if cfg.Server == "" || cfg.Device == "" {
		return nil, fmt.Errorf("invalid config file at %s: missing server or device", path)
	}
	if cfg.Agent == "" {
		cfg.Agent = "claude"
	}
	return cfg, nil
}

func LoadCredentials() (*AgentCredentials, error) {
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

func FindCurrentSession() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		path := filepath.Join(dir, ".agentlink.toml")
		if data, err := os.ReadFile(path); err == nil {
			session := ReadTOML(string(data), "session")
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

func ReadTOML(content, key string) string {
	var section string
	var lookupKey string
	if idx := strings.Index(key, "."); idx >= 0 {
		section = key[:idx]
		lookupKey = key[idx+1:]
	} else {
		lookupKey = key
	}

	prefix := lookupKey + " = "
	inSection := section == ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			trimmed := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			inSection = section != "" && trimmed == section
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.Trim(val, `"`)
			return val
		}
	}
	return ""
}

func ReadTOMLBool(content, key string, defaultVal bool) bool {
	v := ReadTOML(content, key)
	if v == "" {
		return defaultVal
	}
	return v == "true"
}

func ReadTOMLInt(content, key string, defaultVal int) int {
	v := ReadTOML(content, key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

// readTOMLSection parses all key = "value" pairs under [section] into a map.
// Returns nil if the section is absent. Used for [sessions] in config.toml.
func ReadTOMLSection(content, section string) map[string]string {
	result := map[string]string{}
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			trimmed := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			inSection = trimmed == section
			continue
		}
		if !inSection {
			continue
		}
		if idx := strings.Index(line, " = "); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.Trim(strings.TrimSpace(line[idx+3:]), `"`)
			result[key] = val
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// updateSessionID rewrites the [sessions] entry for sessionName with a new
// session_id, preserving all other config fields. If [sessions] is absent,
// it is appended.
func UpdateSessionID(configPath, sessionName, sessionID string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cannot read config: %w", err)
	}
	content := string(data)

	sessions := ReadTOMLSection(content, "sessions")
	if sessions == nil {
		sessions = map[string]string{}
	}
	sessions[sessionName] = sessionID

	// Rebuild: keep everything up to [sessions], then rewrite [sessions].
	var rebuilt strings.Builder
	var beforeSessions strings.Builder
	inSessions := false
	sessionsWritten := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			if sectionName == "sessions" {
				inSessions = true
				if !sessionsWritten {
					beforeSessions.WriteString(BuildSessionsSection(sessions))
					sessionsWritten = true
				}
				continue
			}
			if inSessions {
				inSessions = false
			}
		}
		if inSessions {
			continue
		}
		beforeSessions.WriteString(line)
		beforeSessions.WriteString("\n")
	}

	rebuilt.WriteString(beforeSessions.String())
	if !sessionsWritten {
		rebuilt.WriteString(BuildSessionsSection(sessions))
	}

	return os.WriteFile(configPath, []byte(rebuilt.String()), 0600)
}

func BuildSessionsSection(sessions map[string]string) string {
	var b strings.Builder
	b.WriteString("[sessions]\n")
	for _, name := range sortedSessionKeys(sessions) {
		fmt.Fprintf(&b, "%s = %q\n", name, sessions[name])
	}
	return b.String()
}

func sortedSessionKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WriteConfigTOML writes the agent-level config file. When sessions is
// non-empty, appends a [sessions] segment with the recorded session_ids.
func WriteConfigTOML(path, server, device, baseDir, agent string, noPoll bool, sessions map[string]string) error {
	pollVal := "true"
	if noPoll {
		pollVal = "false"
	}
	content := fmt.Sprintf(`server = %q
device = %q
base_dir = %q
agent = %q

[poll]
enabled = %s
interval = 5
`, server, device, baseDir, agent, pollVal)

	if len(sessions) > 0 {
		content += "\n" + BuildSessionsSection(sessions)
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// WriteSessionTOML writes the per-session .agentlink.toml marker file. The
// agent type is written once at creation and is never rewritten (issue 35).
func WriteSessionTOML(path, session, device, agent string) error {
	content := fmt.Sprintf(`session = %q
device = %q
agent = %q
`, session, device, agent)
	return os.WriteFile(path, []byte(content), 0600)
}

// ReadSessionAgent returns the agent type recorded in <sessionDir>/.agentlink.toml.
// Returns "" if the file or the field is absent (caller falls back to the
// device-level default). The agent type is immutable per session (issue 35).
func ReadSessionAgent(sessionDir string) string {
	data, err := os.ReadFile(filepath.Join(sessionDir, ".agentlink.toml"))
	if err != nil {
		return ""
	}
	return ReadTOML(string(data), "agent")
}

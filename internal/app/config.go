package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	defaultCleanThresholdHours   = 24.0
	defaultDashboardRefreshSecs  = 2
	defaultDoneMessageTemplate   = "agent({id}): {purpose}"
	defaultSnapshotMessageFormat = "snapshot({id}): {timestamp}"
	defaultStopReason            = "manual stop"
)

const (
	configKeyCleanHours              = "agentgit.cleanHours"
	configKeyDashboardRefreshSeconds = "agentgit.dashboardRefreshSeconds"
	configKeyDefaultOwner            = "agentgit.defaultOwner"
	configKeyDoneAuthorName          = "agentgit.doneAuthorName"
	configKeyDoneAuthorEmail         = "agentgit.doneAuthorEmail"
	configKeyDoneMessageTemplate     = "agentgit.doneMessageTemplate"
	configKeySnapshotMessageTemplate = "agentgit.snapshotMessageTemplate"
	configKeyStopReason              = "agentgit.stopReason"
)

type Config struct {
	CleanThresholdHours   float64 `json:"clean_threshold_hours"`
	DashboardRefreshSecs  int     `json:"dashboard_refresh_seconds"`
	DefaultOwner          string  `json:"default_owner,omitempty"`
	DoneAuthorName        string  `json:"done_author_name,omitempty"`
	DoneAuthorEmail       string  `json:"done_author_email,omitempty"`
	DoneMessageTemplate   string  `json:"done_message_template"`
	SnapshotMessageFormat string  `json:"snapshot_message_template"`
	DefaultStopReason     string  `json:"default_stop_reason"`
}

type ConfigInitChange struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Action string `json:"action"`
}

func DefaultConfig() Config {
	return Config{
		CleanThresholdHours:   defaultCleanThresholdHours,
		DashboardRefreshSecs:  defaultDashboardRefreshSecs,
		DoneMessageTemplate:   defaultDoneMessageTemplate,
		SnapshotMessageFormat: defaultSnapshotMessageFormat,
		DefaultStopReason:     defaultStopReason,
	}
}

func (s *Service) InitConfig() ([]ConfigInitChange, error) {
	raw, err := gitConfigMap(s.Repo)
	if err != nil {
		return nil, err
	}

	changes := make([]ConfigInitChange, 0, 8)
	recommended := []ConfigInitChange{
		{Key: configKeyCleanHours, Value: strconv.FormatFloat(defaultCleanThresholdHours, 'f', -1, 64)},
		{Key: configKeyDashboardRefreshSeconds, Value: strconv.Itoa(defaultDashboardRefreshSecs)},
		{Key: configKeyDoneMessageTemplate, Value: defaultDoneMessageTemplate},
		{Key: configKeySnapshotMessageTemplate, Value: defaultSnapshotMessageFormat},
		{Key: configKeyStopReason, Value: defaultStopReason},
	}

	if value, err := gitConfigValue(s.Repo, "user.name"); err != nil {
		return nil, err
	} else if value != "" {
		recommended = append(recommended, ConfigInitChange{Key: configKeyDoneAuthorName, Value: value})
	}
	if value, err := gitConfigValue(s.Repo, "user.email"); err != nil {
		return nil, err
	} else if value != "" {
		recommended = append(recommended, ConfigInitChange{Key: configKeyDoneAuthorEmail, Value: value})
	}

	for _, entry := range recommended {
		if _, ok := raw[strings.ToLower(entry.Key)]; ok {
			entry.Action = "skipped"
			changes = append(changes, entry)
			continue
		}
		if _, err := s.git("", nil, "", "config", "--local", entry.Key, entry.Value); err != nil {
			return nil, err
		}
		entry.Action = "written"
		changes = append(changes, entry)
	}
	return changes, nil
}

func (s *Service) ValidateConfig() error {
	raw, err := gitConfigMap(s.Repo)
	if err != nil {
		return err
	}
	for _, key := range []string{configKeyDoneMessageTemplate, configKeySnapshotMessageTemplate} {
		if value, ok := raw[strings.ToLower(key)]; ok && strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s cannot be empty", key)
		}
	}
	_, err = loadConfig(s.Repo)
	return err
}

func loadConfig(repo string) (Config, error) {
	cfg := DefaultConfig()

	raw, err := gitConfigMap(repo)
	if err != nil {
		return Config{}, err
	}

	if value := firstNonEmpty(os.Getenv("AGENTGIT_CLEAN_HOURS"), raw["agentgit.cleanhours"]); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid clean threshold hours %q", value)
		}
		cfg.CleanThresholdHours = parsed
	}
	if value := firstNonEmpty(os.Getenv("AGENTGIT_DASHBOARD_REFRESH_SECONDS"), raw["agentgit.dashboardrefreshseconds"]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("invalid dashboard refresh seconds %q", value)
		}
		cfg.DashboardRefreshSecs = parsed
	}

	cfg.DefaultOwner = firstNonEmpty(os.Getenv("AGENTGIT_DEFAULT_OWNER"), raw["agentgit.defaultowner"])
	cfg.DoneAuthorName = firstNonEmpty(os.Getenv("AGENTGIT_DONE_AUTHOR_NAME"), raw["agentgit.doneauthorname"])
	cfg.DoneAuthorEmail = firstNonEmpty(os.Getenv("AGENTGIT_DONE_AUTHOR_EMAIL"), raw["agentgit.doneauthoremail"])

	if value := firstNonEmpty(os.Getenv("AGENTGIT_DONE_MESSAGE_TEMPLATE"), raw["agentgit.donemessagetemplate"]); value != "" {
		cfg.DoneMessageTemplate = value
	}
	if value := firstNonEmpty(os.Getenv("AGENTGIT_SNAPSHOT_MESSAGE_TEMPLATE"), raw["agentgit.snapshotmessagetemplate"]); value != "" {
		cfg.SnapshotMessageFormat = value
	}
	if value := firstNonEmpty(os.Getenv("AGENTGIT_STOP_REASON"), raw["agentgit.stopreason"]); value != "" {
		cfg.DefaultStopReason = value
	}

	return cfg, nil
}

func gitConfigMap(repo string) (map[string]string, error) {
	cmd := exec.Command("git", "-C", repo, "config", "--get-regexp", "^agentgit\\.")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 1 {
			return map[string]string{}, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("failed to read git config: %s", msg)
	}

	result := map[string]string{}
	for _, rawLine := range strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		result[key] = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func gitConfigValue(repo, key string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "config", "--get", key)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("failed to read git config %s: %s", key, msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

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

func DefaultConfig() Config {
	return Config{
		CleanThresholdHours:   defaultCleanThresholdHours,
		DashboardRefreshSecs:  defaultDashboardRefreshSecs,
		DoneMessageTemplate:   defaultDoneMessageTemplate,
		SnapshotMessageFormat: defaultSnapshotMessageFormat,
		DefaultStopReason:     defaultStopReason,
	}
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
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

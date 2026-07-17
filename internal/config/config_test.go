package config

import (
	"log/slog"
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DISCORD_TOKEN", "token")
	t.Setenv("YOUTUBE_API_KEY", "key")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PollInterval != 5*time.Minute {
		t.Errorf("PollInterval = %v, want 5m", cfg.PollInterval)
	}
	if cfg.DBPath != "/data/carrierpigeon.db" || cfg.HeartbeatPath != "/data/heartbeat" {
		t.Errorf("paths = %q, %q", cfg.DBPath, cfg.HeartbeatPath)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("POLL_INTERVAL_SECONDS", "60")
	t.Setenv("LOG_LEVEL", "debug")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PollInterval != time.Minute {
		t.Errorf("PollInterval = %v, want 1m", cfg.PollInterval)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	setRequired(t)
	t.Setenv("POLL_INTERVAL_SECONDS", "zero")
	if _, err := Load(); err == nil {
		t.Error("invalid POLL_INTERVAL_SECONDS accepted")
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	setRequired(t)
	t.Setenv("DISCORD_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Error("missing DISCORD_TOKEN accepted")
	}

	setRequired(t)
	t.Setenv("YOUTUBE_API_KEY", "")
	if _, err := Load(); err == nil {
		t.Error("missing YOUTUBE_API_KEY accepted")
	}
}

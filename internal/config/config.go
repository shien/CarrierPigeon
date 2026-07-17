// Package config loads and validates runtime configuration from
// environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DiscordToken  string
	YouTubeAPIKey string
	PollInterval  time.Duration
	DBPath        string
	HeartbeatPath string
	LogLevel      slog.Level
}

func Load() (*Config, error) {
	cfg := &Config{
		DiscordToken:  os.Getenv("DISCORD_TOKEN"),
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
		DBPath:        getenv("DB_PATH", "/data/carrierpigeon.db"),
		HeartbeatPath: getenv("HEARTBEAT_PATH", "/data/heartbeat"),
		PollInterval:  5 * time.Minute,
		LogLevel:      slog.LevelInfo,
	}

	if v := os.Getenv("POLL_INTERVAL_SECONDS"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 1 {
			return nil, fmt.Errorf("invalid POLL_INTERVAL_SECONDS: %q", v)
		}
		cfg.PollInterval = time.Duration(secs) * time.Second
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return nil, fmt.Errorf("invalid LOG_LEVEL: %q", v)
		}
	}

	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is required")
	}
	if cfg.YouTubeAPIKey == "" {
		return nil, fmt.Errorf("YOUTUBE_API_KEY is required")
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

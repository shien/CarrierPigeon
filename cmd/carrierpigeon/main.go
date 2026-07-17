// Command carrierpigeon runs the Discord notification bot for YouTube
// uploads and live streams. It makes outbound connections only, so it
// works in environments with no inbound network access.
//
// Usage:
//
//	carrierpigeon              run the bot
//	carrierpigeon healthcheck  exit 0 if the poller cycled recently
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/api/option"

	"github.com/shien/carrierpigeon/internal/bot"
	"github.com/shien/carrierpigeon/internal/config"
	"github.com/shien/carrierpigeon/internal/heartbeat"
	"github.com/shien/carrierpigeon/internal/notifier"
	"github.com/shien/carrierpigeon/internal/store"
	"github.com/shien/carrierpigeon/internal/watcher/youtube"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		runHealthcheck()
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// runHealthcheck is invoked by the container healthcheck. It only reads
// the heartbeat file, so it needs no network and no database.
func runHealthcheck() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
	// Allow a couple of slow cycles before declaring the poller dead.
	if err := heartbeat.Check(cfg.HeartbeatPath, 3*cfg.PollInterval); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	classifier, err := youtube.NewAPIClassifier(ctx, option.WithAPIKey(cfg.YouTubeAPIKey))
	if err != nil {
		return err
	}

	b, err := bot.New(cfg.DiscordToken, st, classifier, nil /* set below */, log)
	if err != nil {
		return err
	}

	poller := &youtube.Poller{
		Store:    st,
		Feed:     &youtube.RSSFetcher{},
		Class:    classifier,
		Notif:    &notifier.Discord{Session: b.Session},
		Interval: cfg.PollInterval,
		Heartbeat: func() {
			if err := heartbeat.Touch(cfg.HeartbeatPath); err != nil {
				log.Warn("heartbeat touch failed", "error", err)
			}
		},
		Log: log,
	}
	b.Seeder = poller

	if err := b.Open(); err != nil {
		return fmt.Errorf("open discord gateway: %w", err)
	}
	defer b.Close()
	log.Info("carrierpigeon started", "poll_interval", cfg.PollInterval.String())

	err = poller.Run(ctx)
	if ctx.Err() != nil {
		log.Info("shutting down")
		return nil
	}
	return err
}

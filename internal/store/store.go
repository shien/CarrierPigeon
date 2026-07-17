// Package store persists subscriptions and per-video notification state
// in SQLite so that restarts never cause duplicate or missed notifications.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type VideoState string

const (
	StateNone     VideoState = "none"     // regular uploaded video
	StateUpcoming VideoState = "upcoming" // scheduled live stream / premiere
	StateLive     VideoState = "live"     // currently streaming
	StateEnded    VideoState = "ended"    // finished or removed
)

type Subscription struct {
	YTChannelID      string
	YTChannelName    string
	DiscordGuildID   string
	DiscordChannelID string
}

type Video struct {
	ID             string
	YTChannelID    string
	Title          string
	State          VideoState
	ScheduledStart time.Time // zero when unknown
	NotifiedUpload bool
	NotifiedLive   bool
}

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS subscriptions (
  yt_channel_id      TEXT NOT NULL,
  yt_channel_name    TEXT NOT NULL DEFAULT '',
  discord_guild_id   TEXT NOT NULL,
  discord_channel_id TEXT NOT NULL,
  created_at         TEXT NOT NULL,
  PRIMARY KEY (yt_channel_id, discord_channel_id)
);
CREATE TABLE IF NOT EXISTS videos (
  video_id        TEXT PRIMARY KEY,
  yt_channel_id   TEXT NOT NULL,
  title           TEXT NOT NULL DEFAULT '',
  state           TEXT NOT NULL,
  scheduled_start TEXT NOT NULL DEFAULT '',
  notified_upload INTEGER NOT NULL DEFAULT 0,
  notified_live   INTEGER NOT NULL DEFAULT 0,
  updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_videos_state ON videos (state);
`

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows only one writer; serializing through a single
	// connection avoids SQLITE_BUSY between the bot and the poller.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- subscriptions ---

func (s *Store) AddSubscription(ctx context.Context, sub Subscription) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO subscriptions (yt_channel_id, yt_channel_name, discord_guild_id, discord_channel_id, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (yt_channel_id, discord_channel_id)
		DO UPDATE SET yt_channel_name = excluded.yt_channel_name`,
		sub.YTChannelID, sub.YTChannelName, sub.DiscordGuildID, sub.DiscordChannelID,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// RemoveSubscription deletes every subscription of a YouTube channel in a
// guild and reports whether anything was removed.
func (s *Store) RemoveSubscription(ctx context.Context, guildID, ytChannelID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM subscriptions WHERE discord_guild_id = ? AND yt_channel_id = ?`,
		guildID, ytChannelID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ListSubscriptionsByGuild(ctx context.Context, guildID string) ([]Subscription, error) {
	return s.querySubscriptions(ctx,
		`SELECT yt_channel_id, yt_channel_name, discord_guild_id, discord_channel_id
		 FROM subscriptions WHERE discord_guild_id = ? ORDER BY yt_channel_name`, guildID)
}

// TargetsFor returns every subscription watching the given YouTube channel.
func (s *Store) TargetsFor(ctx context.Context, ytChannelID string) ([]Subscription, error) {
	return s.querySubscriptions(ctx,
		`SELECT yt_channel_id, yt_channel_name, discord_guild_id, discord_channel_id
		 FROM subscriptions WHERE yt_channel_id = ?`, ytChannelID)
}

// WatchedChannels returns the distinct YouTube channel IDs that have at
// least one subscription.
func (s *Store) WatchedChannels(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT yt_channel_id FROM subscriptions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) HasChannel(ctx context.Context, ytChannelID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM subscriptions WHERE yt_channel_id = ?`, ytChannelID).Scan(&n)
	return n > 0, err
}

func (s *Store) querySubscriptions(ctx context.Context, query string, args ...any) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.YTChannelID, &sub.YTChannelName, &sub.DiscordGuildID, &sub.DiscordChannelID); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// --- videos ---

func (s *Store) UpsertVideo(ctx context.Context, v Video) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO videos (video_id, yt_channel_id, title, state, scheduled_start, notified_upload, notified_live, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (video_id) DO UPDATE SET
		  title = excluded.title,
		  state = excluded.state,
		  scheduled_start = excluded.scheduled_start,
		  notified_upload = excluded.notified_upload,
		  notified_live = excluded.notified_live,
		  updated_at = excluded.updated_at`,
		v.ID, v.YTChannelID, v.Title, string(v.State), formatTime(v.ScheduledStart),
		boolToInt(v.NotifiedUpload), boolToInt(v.NotifiedLive),
		time.Now().UTC().Format(time.RFC3339))
	return err
}

// FilterUnknown returns the subset of ids that has no row in videos yet.
func (s *Store) FilterUnknown(ctx context.Context, ids []string) ([]string, error) {
	var unknown []string
	for _, id := range ids {
		var n int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM videos WHERE video_id = ?`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			unknown = append(unknown, id)
		}
	}
	return unknown, nil
}

// PendingVideos returns videos that still need action from the poller:
// an unsent upload notification, or a tracked upcoming/live stream whose
// start has not been announced yet.
func (s *Store) PendingVideos(ctx context.Context) ([]Video, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT video_id, yt_channel_id, title, state, scheduled_start, notified_upload, notified_live
		FROM videos
		WHERE notified_upload = 0
		   OR (state IN ('upcoming', 'live') AND notified_live = 0)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var videos []Video
	for rows.Next() {
		var v Video
		var state, sched string
		var nu, nl int
		if err := rows.Scan(&v.ID, &v.YTChannelID, &v.Title, &state, &sched, &nu, &nl); err != nil {
			return nil, err
		}
		v.State = VideoState(state)
		v.ScheduledStart = parseTime(sched)
		v.NotifiedUpload = nu != 0
		v.NotifiedLive = nl != 0
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

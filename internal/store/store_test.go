package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSubscriptionCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	sub := Subscription{YTChannelID: "UCaaa", YTChannelName: "Ch A", DiscordGuildID: "g1", DiscordChannelID: "d1"}
	if err := s.AddSubscription(ctx, sub); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Same key twice must not error (upsert) and must not duplicate.
	sub.YTChannelName = "Ch A renamed"
	if err := s.AddSubscription(ctx, sub); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if err := s.AddSubscription(ctx, Subscription{YTChannelID: "UCbbb", DiscordGuildID: "g1", DiscordChannelID: "d2"}); err != nil {
		t.Fatalf("add second: %v", err)
	}

	subs, err := s.ListSubscriptionsByGuild(ctx, "g1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("want 2 subscriptions, got %d", len(subs))
	}

	channels, err := s.WatchedChannels(ctx)
	if err != nil {
		t.Fatalf("watched: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("want 2 watched channels, got %d", len(channels))
	}

	if ok, _ := s.HasChannel(ctx, "UCaaa"); !ok {
		t.Error("HasChannel(UCaaa) = false, want true")
	}
	if ok, _ := s.HasChannel(ctx, "UCzzz"); ok {
		t.Error("HasChannel(UCzzz) = true, want false")
	}

	removed, err := s.RemoveSubscription(ctx, "g1", "UCaaa")
	if err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	removed, err = s.RemoveSubscription(ctx, "g1", "UCaaa")
	if err != nil || removed {
		t.Fatalf("second remove: removed=%v err=%v, want false", removed, err)
	}
}

func TestTargetsForSpansGuilds(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	s.AddSubscription(ctx, Subscription{YTChannelID: "UCaaa", DiscordGuildID: "g1", DiscordChannelID: "d1"})
	s.AddSubscription(ctx, Subscription{YTChannelID: "UCaaa", DiscordGuildID: "g2", DiscordChannelID: "d2"})

	targets, err := s.TargetsFor(ctx, "UCaaa")
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("want 2 targets across guilds, got %d", len(targets))
	}
}

func TestVideoStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	sched := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	v := Video{ID: "vid1", YTChannelID: "UCaaa", Title: "t", State: StateUpcoming, ScheduledStart: sched}
	if err := s.UpsertVideo(ctx, v); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	unknown, err := s.FilterUnknown(ctx, []string{"vid1", "vid2"})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(unknown) != 1 || unknown[0] != "vid2" {
		t.Fatalf("FilterUnknown = %v, want [vid2]", unknown)
	}

	pending, err := s.PendingVideos(ctx)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending video, got %d", len(pending))
	}
	got := pending[0]
	if got.State != StateUpcoming || !got.ScheduledStart.Equal(sched) || got.NotifiedUpload || got.NotifiedLive {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestPendingVideosSelection(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	cases := []Video{
		{ID: "unnotified-upload", State: StateNone},                                     // pending
		{ID: "done-upload", State: StateNone, NotifiedUpload: true, NotifiedLive: true}, // not pending
		{ID: "waiting-live", State: StateUpcoming, NotifiedUpload: true},                // pending
		{ID: "live-unannounced", State: StateLive, NotifiedUpload: true},                // pending
		{ID: "live-announced", State: StateLive, NotifiedUpload: true, NotifiedLive: true},
		{ID: "over", State: StateEnded, NotifiedUpload: true, NotifiedLive: true},
	}
	for _, v := range cases {
		v.YTChannelID = "UCaaa"
		if err := s.UpsertVideo(ctx, v); err != nil {
			t.Fatalf("upsert %s: %v", v.ID, err)
		}
	}

	pending, err := s.PendingVideos(ctx)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	got := map[string]bool{}
	for _, v := range pending {
		got[v.ID] = true
	}
	want := []string{"unnotified-upload", "waiting-live", "live-unannounced"}
	if len(got) != len(want) {
		t.Fatalf("pending = %v, want %v", got, want)
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("%s missing from pending set", id)
		}
	}
}

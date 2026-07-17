package youtube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shien/carrierpigeon/internal/store"
)

// Poller runs the polling cycle. Notification flags are only persisted
// after Discord accepts the message, so a failed send is retried on the
// next cycle instead of being lost.
type Poller struct {
	Store     *store.Store
	Feed      FeedFetcher
	Class     Classifier
	Notif     Notifier
	Interval  time.Duration
	Heartbeat func() // called after every completed cycle; may be nil
	Log       *slog.Logger
}

func (p *Poller) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		if err := p.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			p.log().Error("poll cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce executes one polling cycle:
//
//  1. discover: RSS per watched channel -> classify unseen IDs -> store
//  2. dispatch: notify pending videos and upcoming->live transitions
//
// A failure on one channel is logged and does not block the others.
func (p *Poller) RunOnce(ctx context.Context) error {
	channels, err := p.Store.WatchedChannels(ctx)
	if err != nil {
		return fmt.Errorf("list watched channels: %w", err)
	}

	fresh := map[string]Video{} // classifications already fetched this cycle
	for _, ch := range channels {
		if err := p.discover(ctx, ch, fresh); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			p.log().Error("discover failed", "channel", ch, "error", err)
		}
	}

	if err := p.dispatch(ctx, fresh); err != nil {
		return err
	}

	if p.Heartbeat != nil {
		p.Heartbeat()
	}
	return nil
}

// discover finds videos that are in the RSS feed but not in the store,
// classifies them, and records them with notification flags unset.
func (p *Poller) discover(ctx context.Context, channelID string, fresh map[string]Video) error {
	entries, err := p.Feed.Fetch(ctx, channelID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.VideoID)
	}
	unknown, err := p.Store.FilterUnknown(ctx, ids)
	if err != nil {
		return err
	}
	if len(unknown) == 0 {
		return nil
	}

	infos, err := p.Class.Classify(ctx, unknown)
	if err != nil {
		return err
	}
	for _, id := range unknown {
		info, ok := infos[id]
		if !ok {
			// In the feed but not in the API (already deleted or
			// private): record it so we never ask again.
			info = Video{ID: id, ChannelID: channelID, State: store.StateEnded}
			if err := p.Store.UpsertVideo(ctx, store.Video{
				ID: id, YTChannelID: channelID, State: store.StateEnded,
				NotifiedUpload: true, NotifiedLive: true,
			}); err != nil {
				return err
			}
			continue
		}
		fresh[id] = info
		if err := p.Store.UpsertVideo(ctx, toStoreVideo(info, false, false)); err != nil {
			return err
		}
		p.log().Info("discovered video", "video", id, "channel", channelID, "state", info.State)
	}
	return nil
}

// dispatch sends notifications for pending videos and tracks the
// upcoming->live transition. `fresh` avoids re-querying videos that were
// classified moments ago in discover.
func (p *Poller) dispatch(ctx context.Context, fresh map[string]Video) error {
	pending, err := p.Store.PendingVideos(ctx)
	if err != nil {
		return fmt.Errorf("list pending videos: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	var stale []string
	for _, v := range pending {
		if _, ok := fresh[v.ID]; !ok {
			stale = append(stale, v.ID)
		}
	}
	infos := fresh
	if len(stale) > 0 {
		refreshed, err := p.Class.Classify(ctx, stale)
		if err != nil {
			return fmt.Errorf("refresh pending videos: %w", err)
		}
		infos = make(map[string]Video, len(fresh)+len(refreshed))
		for id, v := range fresh {
			infos[id] = v
		}
		for id, v := range refreshed {
			infos[id] = v
		}
	}

	for _, rec := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, ok := infos[rec.ID]
		if !ok {
			// Deleted while pending: stop tracking silently.
			rec.State = store.StateEnded
			rec.NotifiedUpload, rec.NotifiedLive = true, true
			if err := p.Store.UpsertVideo(ctx, rec); err != nil {
				return err
			}
			continue
		}
		if err := p.dispatchOne(ctx, rec, info); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			p.log().Error("notify failed, will retry next cycle", "video", rec.ID, "error", err)
		}
	}
	return nil
}

func (p *Poller) dispatchOne(ctx context.Context, rec store.Video, info Video) error {
	if info.ChannelID == "" {
		info.ChannelID = rec.YTChannelID
	}

	switch {
	case !rec.NotifiedUpload:
		var event Event
		switch info.State {
		case store.StateUpcoming:
			event = EventUpcoming
		case store.StateLive:
			event = EventLiveStarted
		default:
			event = EventNewVideo
		}
		if err := p.notifyTargets(ctx, event, info); err != nil {
			return err
		}
		notifiedLive := info.State != store.StateUpcoming // upcoming keeps waiting for its live event
		return p.Store.UpsertVideo(ctx, toStoreVideo(info, true, notifiedLive))

	case rec.State == store.StateUpcoming || rec.State == store.StateLive:
		switch info.State {
		case store.StateLive:
			if err := p.notifyTargets(ctx, EventLiveStarted, info); err != nil {
				return err
			}
			return p.Store.UpsertVideo(ctx, toStoreVideo(info, true, true))
		case store.StateUpcoming:
			return nil // still waiting
		default:
			// Cancelled, or ended before we saw it live: stop tracking.
			return p.Store.UpsertVideo(ctx, toStoreVideo(info, true, true))
		}
	}
	return nil
}

func (p *Poller) notifyTargets(ctx context.Context, event Event, info Video) error {
	targets, err := p.Store.TargetsFor(ctx, info.ChannelID)
	if err != nil {
		return err
	}
	var errs []error
	for _, t := range targets {
		if err := p.Notif.Notify(ctx, t.DiscordChannelID, event, info); err != nil {
			errs = append(errs, fmt.Errorf("notify %s: %w", t.DiscordChannelID, err))
		}
	}
	return errors.Join(errs...)
}

// Seed records a channel's current feed as already-notified so that a new
// subscription does not flood Discord with old videos. Streams that are
// currently scheduled stay tracked so their start is still announced.
func (p *Poller) Seed(ctx context.Context, channelID string) error {
	entries, err := p.Feed.Fetch(ctx, channelID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.VideoID)
	}
	unknown, err := p.Store.FilterUnknown(ctx, ids)
	if err != nil || len(unknown) == 0 {
		return err
	}

	infos, err := p.Class.Classify(ctx, unknown)
	if err != nil {
		// Degraded seeding: without classification, mark everything
		// as a fully notified regular video rather than failing the
		// subscribe command.
		p.log().Warn("seed classification failed, seeding without live tracking", "channel", channelID, "error", err)
		infos = map[string]Video{}
	}
	for _, id := range unknown {
		v := store.Video{ID: id, YTChannelID: channelID, State: store.StateNone, NotifiedUpload: true, NotifiedLive: true}
		if info, ok := infos[id]; ok {
			v = toStoreVideo(info, true, info.State != store.StateUpcoming)
		}
		if err := p.Store.UpsertVideo(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

func toStoreVideo(v Video, notifiedUpload, notifiedLive bool) store.Video {
	return store.Video{
		ID:             v.ID,
		YTChannelID:    v.ChannelID,
		Title:          v.Title,
		State:          v.State,
		ScheduledStart: v.ScheduledStart,
		NotifiedUpload: notifiedUpload,
		NotifiedLive:   notifiedLive,
	}
}

func (p *Poller) log() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

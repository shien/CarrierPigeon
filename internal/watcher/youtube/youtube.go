// Package youtube detects new uploads and live streams with a two-stage,
// outbound-only strategy: the free RSS feed discovers new video IDs and
// the Data API classifies them (upload / upcoming / live), keeping quota
// usage to a handful of units per polling cycle.
package youtube

import (
	"context"
	"time"

	"github.com/shien/carrierpigeon/internal/store"
)

// Video is everything the notifier needs to render an embed.
type Video struct {
	ID             string
	ChannelID      string
	Title          string
	ChannelTitle   string
	Thumbnail      string
	State          store.VideoState
	ScheduledStart time.Time
}

func (v Video) URL() string { return "https://www.youtube.com/watch?v=" + v.ID }

// Event is the kind of notification to deliver.
type Event int

const (
	EventNewVideo    Event = iota // regular upload published
	EventUpcoming                 // stream/premiere scheduled
	EventLiveStarted              // stream/premiere went live
)

// FeedEntry is one item from the RSS feed.
type FeedEntry struct {
	VideoID   string
	Title     string
	Published time.Time
}

// FeedFetcher discovers video IDs without spending API quota.
type FeedFetcher interface {
	Fetch(ctx context.Context, channelID string) ([]FeedEntry, error)
}

// Classifier resolves video IDs into their current broadcast state.
type Classifier interface {
	// Classify returns a map keyed by video ID. IDs missing from the
	// map no longer exist (deleted or private).
	Classify(ctx context.Context, ids []string) (map[string]Video, error)
}

// Notifier delivers one event to one Discord channel.
type Notifier interface {
	Notify(ctx context.Context, discordChannelID string, event Event, video Video) error
}

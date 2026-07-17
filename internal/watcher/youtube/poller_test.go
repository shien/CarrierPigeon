package youtube

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shien/carrierpigeon/internal/store"
)

// --- fakes ---

type fakeFeed struct {
	entries map[string][]FeedEntry // by channel ID
}

func (f *fakeFeed) Fetch(_ context.Context, ch string) ([]FeedEntry, error) {
	return f.entries[ch], nil
}

type fakeClassifier struct {
	videos map[string]Video
	calls  int
}

func (f *fakeClassifier) Classify(_ context.Context, ids []string) (map[string]Video, error) {
	f.calls++
	out := map[string]Video{}
	for _, id := range ids {
		if v, ok := f.videos[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

type sentEvent struct {
	DiscordChannel string
	Event          Event
	VideoID        string
}

type fakeNotifier struct {
	sent    []sentEvent
	failVid string // Notify fails while the video has this ID
}

func (f *fakeNotifier) Notify(_ context.Context, discordCh string, ev Event, v Video) error {
	if v.ID == f.failVid {
		return errors.New("discord unavailable")
	}
	f.sent = append(f.sent, sentEvent{discordCh, ev, v.ID})
	return nil
}

func newPoller(t *testing.T) (*Poller, *fakeFeed, *fakeClassifier, *fakeNotifier) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	feed := &fakeFeed{entries: map[string][]FeedEntry{}}
	class := &fakeClassifier{videos: map[string]Video{}}
	notif := &fakeNotifier{}
	p := &Poller{Store: st, Feed: feed, Class: class, Notif: notif, Interval: time.Minute}
	return p, feed, class, notif
}

func subscribe(t *testing.T, p *Poller, ytChannel, discordChannel string) {
	t.Helper()
	err := p.Store.AddSubscription(context.Background(), store.Subscription{
		YTChannelID: ytChannel, DiscordGuildID: "g1", DiscordChannelID: discordChannel,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
}

// --- tests ---

func TestNewVideoNotifiedExactlyOnce(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "vid1", Title: "New upload"}}
	class.videos["vid1"] = Video{ID: "vid1", ChannelID: "UCaaa", Title: "New upload", State: store.StateNone}

	for i := 0; i < 3; i++ {
		if err := p.RunOnce(ctx); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}

	if len(notif.sent) != 1 {
		t.Fatalf("sent %d notifications, want exactly 1: %+v", len(notif.sent), notif.sent)
	}
	got := notif.sent[0]
	if got.Event != EventNewVideo || got.VideoID != "vid1" || got.DiscordChannel != "d1" {
		t.Errorf("unexpected notification: %+v", got)
	}
}

func TestUpcomingThenLiveNotifiesEachOnce(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "stream1"}}
	class.videos["stream1"] = Video{
		ID: "stream1", ChannelID: "UCaaa", Title: "Stream", State: store.StateUpcoming,
		ScheduledStart: time.Now().Add(time.Hour),
	}

	// Two cycles while scheduled: exactly one "upcoming" notification.
	for i := 0; i < 2; i++ {
		if err := p.RunOnce(ctx); err != nil {
			t.Fatalf("upcoming cycle %d: %v", i, err)
		}
	}
	if len(notif.sent) != 1 || notif.sent[0].Event != EventUpcoming {
		t.Fatalf("after upcoming cycles: %+v, want single EventUpcoming", notif.sent)
	}

	// The stream goes live: exactly one "live" notification, then quiet.
	class.videos["stream1"] = Video{ID: "stream1", ChannelID: "UCaaa", Title: "Stream", State: store.StateLive}
	for i := 0; i < 2; i++ {
		if err := p.RunOnce(ctx); err != nil {
			t.Fatalf("live cycle %d: %v", i, err)
		}
	}
	if len(notif.sent) != 2 || notif.sent[1].Event != EventLiveStarted {
		t.Fatalf("after live cycles: %+v, want EventUpcoming then EventLiveStarted", notif.sent)
	}

	// Stream ends (state back to none): no further notifications, and the
	// video stops being re-classified once settled.
	class.videos["stream1"] = Video{ID: "stream1", ChannelID: "UCaaa", State: store.StateNone}
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("ended cycle: %v", err)
	}
	callsAfterEnd := class.calls
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("settled cycle: %v", err)
	}
	if len(notif.sent) != 2 {
		t.Fatalf("stream end produced extra notifications: %+v", notif.sent)
	}
	if class.calls != callsAfterEnd {
		t.Errorf("settled video still consumes API calls")
	}
}

func TestGuerrillaStreamAlreadyLiveOnDiscovery(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "guerrilla"}}
	class.videos["guerrilla"] = Video{ID: "guerrilla", ChannelID: "UCaaa", State: store.StateLive}

	for i := 0; i < 2; i++ {
		if err := p.RunOnce(ctx); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}
	if len(notif.sent) != 1 || notif.sent[0].Event != EventLiveStarted {
		t.Fatalf("want a single EventLiveStarted, got %+v", notif.sent)
	}
}

func TestFailedNotificationIsRetried(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "vid1"}}
	class.videos["vid1"] = Video{ID: "vid1", ChannelID: "UCaaa", State: store.StateNone}

	notif.failVid = "vid1"
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("failing cycle: %v", err) // notify failure is logged, not fatal
	}
	if len(notif.sent) != 0 {
		t.Fatalf("notification recorded despite failure: %+v", notif.sent)
	}

	notif.failVid = ""
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if len(notif.sent) != 1 || notif.sent[0].VideoID != "vid1" {
		t.Fatalf("retry did not deliver: %+v", notif.sent)
	}
}

func TestSeedSuppressesBacklogButTracksUpcoming(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "old1"}, {VideoID: "old2"}, {VideoID: "scheduled"}}
	class.videos["old1"] = Video{ID: "old1", ChannelID: "UCaaa", State: store.StateNone}
	class.videos["old2"] = Video{ID: "old2", ChannelID: "UCaaa", State: store.StateNone}
	class.videos["scheduled"] = Video{ID: "scheduled", ChannelID: "UCaaa", State: store.StateUpcoming}

	// Seed first (as /subscribe does), then subscribe and poll.
	if err := p.Seed(ctx, "UCaaa"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	subscribe(t, p, "UCaaa", "d1")
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(notif.sent) != 0 {
		t.Fatalf("seeded backlog was announced: %+v", notif.sent)
	}

	// The pre-existing scheduled stream goes live -> announced.
	class.videos["scheduled"] = Video{ID: "scheduled", ChannelID: "UCaaa", State: store.StateLive}
	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("live cycle: %v", err)
	}
	if len(notif.sent) != 1 || notif.sent[0].Event != EventLiveStarted || notif.sent[0].VideoID != "scheduled" {
		t.Fatalf("want live start for seeded upcoming stream, got %+v", notif.sent)
	}
}

func TestFanOutToMultipleDiscordChannels(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")
	subscribe(t, p, "UCaaa", "d2")

	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "vid1"}}
	class.videos["vid1"] = Video{ID: "vid1", ChannelID: "UCaaa", State: store.StateNone}

	if err := p.RunOnce(ctx); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(notif.sent) != 2 {
		t.Fatalf("want fan-out to 2 channels, got %+v", notif.sent)
	}
	seen := map[string]bool{}
	for _, s := range notif.sent {
		seen[s.DiscordChannel] = true
	}
	if !seen["d1"] || !seen["d2"] {
		t.Errorf("fan-out missed a channel: %+v", notif.sent)
	}
}

func TestDeletedVideoStopsBeingTracked(t *testing.T) {
	ctx := context.Background()
	p, feed, class, notif := newPoller(t)
	subscribe(t, p, "UCaaa", "d1")

	// In the feed but the API doesn't know it (deleted/private).
	feed.entries["UCaaa"] = []FeedEntry{{VideoID: "ghost"}}

	for i := 0; i < 2; i++ {
		if err := p.RunOnce(ctx); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
	}
	if len(notif.sent) != 0 {
		t.Fatalf("ghost video was announced: %+v", notif.sent)
	}
	// Classified once during discovery, never again afterwards.
	if class.calls > 1 {
		t.Errorf("ghost video re-queried %d times, want 1", class.calls)
	}
}

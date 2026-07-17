package notifier

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shien/carrierpigeon/internal/store"
	"github.com/shien/carrierpigeon/internal/watcher/youtube"
)

func TestBuildEmbed(t *testing.T) {
	sched := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	v := youtube.Video{
		ID:             "vid123",
		ChannelID:      "UCxxx",
		Title:          "Some Title",
		ChannelTitle:   "Some Channel",
		Thumbnail:      "https://i.ytimg.com/vi/vid123/hq.jpg",
		State:          store.StateUpcoming,
		ScheduledStart: sched,
	}

	for _, tc := range []struct {
		event youtube.Event
		want  string
	}{
		{youtube.EventNewVideo, "新しい動画"},
		{youtube.EventUpcoming, "配信予定"},
		{youtube.EventLiveStarted, "ライブ配信"},
	} {
		e := BuildEmbed(tc.event, v)
		if e.Title != "Some Title" {
			t.Errorf("event %v: title = %q", tc.event, e.Title)
		}
		if e.URL != "https://www.youtube.com/watch?v=vid123" {
			t.Errorf("event %v: url = %q", tc.event, e.URL)
		}
		if e.Author == nil || e.Author.Name != "Some Channel" {
			t.Errorf("event %v: author = %+v", tc.event, e.Author)
		}
		if e.Image == nil || e.Image.URL != v.Thumbnail {
			t.Errorf("event %v: image = %+v", tc.event, e.Image)
		}
		if !strings.Contains(e.Description, tc.want) {
			t.Errorf("event %v: description %q does not mention %q", tc.event, e.Description, tc.want)
		}
	}
}

func TestBuildEmbedUpcomingSchedule(t *testing.T) {
	sched := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	e := BuildEmbed(youtube.EventUpcoming, youtube.Video{ID: "v", ScheduledStart: sched})
	if want := fmt.Sprintf("<t:%d:F>", sched.Unix()); !strings.Contains(e.Description, want) {
		t.Errorf("description %q missing discord timestamp %q", e.Description, want)
	}

	// Without a schedule the description must still render.
	e = BuildEmbed(youtube.EventUpcoming, youtube.Video{ID: "v"})
	if strings.Contains(e.Description, "<t:") {
		t.Errorf("description %q renders a timestamp for zero time", e.Description)
	}
}

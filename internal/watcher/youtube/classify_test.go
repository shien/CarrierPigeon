package youtube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/option"

	"github.com/shien/carrierpigeon/internal/store"
)

const videosListResponse = `{
  "kind": "youtube#videoListResponse",
  "items": [
    {
      "id": "normalvid01",
      "snippet": {
        "title": "A normal upload",
        "channelId": "UCxxxxxxxxxxxxxxxxxxxxxx",
        "channelTitle": "Sample Channel",
        "liveBroadcastContent": "none",
        "thumbnails": {"high": {"url": "https://i.ytimg.com/vi/normalvid01/hq.jpg"}}
      }
    },
    {
      "id": "upcoming001",
      "snippet": {
        "title": "Scheduled stream",
        "channelId": "UCxxxxxxxxxxxxxxxxxxxxxx",
        "channelTitle": "Sample Channel",
        "liveBroadcastContent": "upcoming"
      },
      "liveStreamingDetails": {"scheduledStartTime": "2026-07-20T12:00:00Z"}
    },
    {
      "id": "livenow0001",
      "snippet": {
        "title": "Live now",
        "channelId": "UCxxxxxxxxxxxxxxxxxxxxxx",
        "channelTitle": "Sample Channel",
        "liveBroadcastContent": "live"
      },
      "liveStreamingDetails": {"scheduledStartTime": "2026-07-17T09:00:00Z", "actualStartTime": "2026-07-17T09:01:00Z"}
    }
  ]
}`

func newTestClassifier(t *testing.T, handler http.HandlerFunc) *APIClassifier {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewAPIClassifier(context.Background(),
		option.WithHTTPClient(srv.Client()), option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatalf("new classifier: %v", err)
	}
	return c
}

func TestClassifyMapsBroadcastStates(t *testing.T) {
	c := newTestClassifier(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, videosListResponse)
	})

	got, err := c.Classify(context.Background(), []string{"normalvid01", "upcoming001", "livenow0001", "deleted0001"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	if v := got["normalvid01"]; v.State != store.StateNone || v.Title != "A normal upload" || v.Thumbnail == "" {
		t.Errorf("normal video mapped wrong: %+v", v)
	}
	if v := got["upcoming001"]; v.State != store.StateUpcoming ||
		!v.ScheduledStart.Equal(time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("upcoming mapped wrong: %+v", v)
	}
	if v := got["livenow0001"]; v.State != store.StateLive {
		t.Errorf("live mapped wrong: %+v", v)
	}
	if _, ok := got["deleted0001"]; ok {
		t.Error("deleted video must be absent from the result map")
	}
}

func TestClassifyBatchesOver50IDs(t *testing.T) {
	var calls int
	c := newTestClassifier(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		// The client joins IDs with commas in a single param.
		ids := strings.Split(r.URL.Query().Get("id"), ",")
		if len(ids) > 50 {
			t.Errorf("batch of %d exceeds API limit of 50", len(ids))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items": []}`)
	})

	ids := make([]string, 120)
	for i := range ids {
		ids[i] = fmt.Sprintf("vid%08d", i)
	}
	if _, err := c.Classify(context.Background(), ids); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if calls != 3 {
		t.Errorf("120 ids made %d calls, want 3 (50+50+20)", calls)
	}
}

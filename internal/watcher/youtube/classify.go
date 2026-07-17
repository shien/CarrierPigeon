package youtube

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/option"
	yt "google.golang.org/api/youtube/v3"

	"github.com/shien/carrierpigeon/internal/store"
)

const classifyBatchSize = 50 // videos.list accepts up to 50 IDs per call (1 quota unit)

// APIClassifier queries the YouTube Data API v3.
type APIClassifier struct {
	svc *yt.Service
}

func NewAPIClassifier(ctx context.Context, opts ...option.ClientOption) (*APIClassifier, error) {
	svc, err := yt.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create youtube service: %w", err)
	}
	return &APIClassifier{svc: svc}, nil
}

func (c *APIClassifier) Classify(ctx context.Context, ids []string) (map[string]Video, error) {
	result := make(map[string]Video, len(ids))
	for start := 0; start < len(ids); start += classifyBatchSize {
		batch := ids[start:min(start+classifyBatchSize, len(ids))]
		resp, err := c.svc.Videos.List([]string{"snippet", "liveStreamingDetails"}).
			Id(batch...).MaxResults(int64(len(batch))).Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("videos.list: %w", err)
		}
		for _, item := range resp.Items {
			result[item.Id] = videoFromAPI(item)
		}
	}
	return result, nil
}

func videoFromAPI(item *yt.Video) Video {
	v := Video{ID: item.Id, State: store.StateNone}
	if sn := item.Snippet; sn != nil {
		v.Title = sn.Title
		v.ChannelID = sn.ChannelId
		v.ChannelTitle = sn.ChannelTitle
		switch sn.LiveBroadcastContent {
		case "upcoming":
			v.State = store.StateUpcoming
		case "live":
			v.State = store.StateLive
		}
		if th := sn.Thumbnails; th != nil {
			for _, t := range []*yt.Thumbnail{th.Maxres, th.High, th.Medium, th.Default} {
				if t != nil && t.Url != "" {
					v.Thumbnail = t.Url
					break
				}
			}
		}
	}
	if det := item.LiveStreamingDetails; det != nil && det.ScheduledStartTime != "" {
		if t, err := time.Parse(time.RFC3339, det.ScheduledStartTime); err == nil {
			v.ScheduledStart = t
		}
	}
	return v
}

// ResolveChannel turns user input (UC... ID, channel URL, @handle, or
// legacy /user/ URL) into a channel ID and title. Costs at most 1 unit.
func (c *APIClassifier) ResolveChannel(ctx context.Context, input string) (id, title string, err error) {
	ref, err := ParseChannelInput(input)
	if err != nil {
		return "", "", err
	}

	call := c.svc.Channels.List([]string{"snippet"}).MaxResults(1).Context(ctx)
	switch ref.Kind {
	case ChannelRefID:
		call = call.Id(ref.Value)
	case ChannelRefHandle:
		call = call.ForHandle(ref.Value)
	case ChannelRefUsername:
		call = call.ForUsername(ref.Value)
	}
	resp, err := call.Do()
	if err != nil {
		return "", "", fmt.Errorf("channels.list: %w", err)
	}
	if len(resp.Items) == 0 {
		return "", "", fmt.Errorf("channel not found: %s", input)
	}
	ch := resp.Items[0]
	if ch.Snippet != nil {
		title = ch.Snippet.Title
	}
	return ch.Id, title, nil
}

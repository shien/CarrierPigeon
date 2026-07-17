package youtube

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mmcdole/gofeed"
)

const defaultFeedBase = "https://www.youtube.com/feeds/videos.xml"

// RSSFetcher reads a channel's Atom feed. It costs no API quota and
// returns roughly the 15 most recent videos.
type RSSFetcher struct {
	Client  *http.Client
	BaseURL string // overridable for tests; defaults to youtube.com
}

func (f *RSSFetcher) Fetch(ctx context.Context, channelID string) ([]FeedEntry, error) {
	base := f.BaseURL
	if base == "" {
		base = defaultFeedBase
	}
	feedURL := base + "?channel_id=" + url.QueryEscape(channelID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed %s: %w", channelID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch feed %s: status %d", channelID, resp.StatusCode)
	}

	feed, err := gofeed.NewParser().Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse feed %s: %w", channelID, err)
	}

	entries := make([]FeedEntry, 0, len(feed.Items))
	for _, item := range feed.Items {
		id := videoIDOf(item)
		if id == "" {
			continue
		}
		entry := FeedEntry{VideoID: id, Title: item.Title}
		if item.PublishedParsed != nil {
			entry.Published = *item.PublishedParsed
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// videoIDOf extracts the video ID from the yt:videoId extension, falling
// back to the ?v= query parameter of the entry link.
func videoIDOf(item *gofeed.Item) string {
	if yt, ok := item.Extensions["yt"]; ok {
		if ids, ok := yt["videoId"]; ok && len(ids) > 0 {
			return ids[0].Value
		}
	}
	if u, err := url.Parse(item.Link); err == nil {
		return u.Query().Get("v")
	}
	return ""
}

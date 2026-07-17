package youtube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"
      xmlns:media="http://search.yahoo.com/mrss/"
      xmlns="http://www.w3.org/2005/Atom">
 <title>Sample Channel</title>
 <entry>
  <id>yt:video:AAAAAAAAAAA</id>
  <yt:videoId>AAAAAAAAAAA</yt:videoId>
  <yt:channelId>UCxxxxxxxxxxxxxxxxxxxxxx</yt:channelId>
  <title>First video</title>
  <link rel="alternate" href="https://www.youtube.com/watch?v=AAAAAAAAAAA"/>
  <published>2026-07-15T10:00:00+00:00</published>
 </entry>
 <entry>
  <id>yt:video:BBBBBBBBBBB</id>
  <yt:videoId>BBBBBBBBBBB</yt:videoId>
  <yt:channelId>UCxxxxxxxxxxxxxxxxxxxxxx</yt:channelId>
  <title>Second video</title>
  <link rel="alternate" href="https://www.youtube.com/watch?v=BBBBBBBBBBB"/>
  <published>2026-07-16T10:00:00+00:00</published>
 </entry>
</feed>`

func TestRSSFetcherParsesFeed(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("channel_id")
		w.Header().Set("Content-Type", "application/atom+xml")
		fmt.Fprint(w, sampleFeed)
	}))
	defer srv.Close()

	f := &RSSFetcher{Client: srv.Client(), BaseURL: srv.URL}
	entries, err := f.Fetch(context.Background(), "UCxxxxxxxxxxxxxxxxxxxxxx")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotQuery != "UCxxxxxxxxxxxxxxxxxxxxxx" {
		t.Errorf("channel_id query = %q", gotQuery)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].VideoID != "AAAAAAAAAAA" || entries[0].Title != "First video" {
		t.Errorf("entry[0] = %+v", entries[0])
	}
	if entries[0].Published.IsZero() {
		t.Error("published time not parsed")
	}
}

func TestRSSFetcherHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	f := &RSSFetcher{Client: srv.Client(), BaseURL: srv.URL}
	if _, err := f.Fetch(context.Background(), "UCgone"); err == nil {
		t.Fatal("want error on 404, got nil")
	}
}

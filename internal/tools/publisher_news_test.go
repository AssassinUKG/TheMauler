package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPublisherHeadlineRoutingAndDatedEvidence(t *testing.T) {
	for _, query := range []string{"What’s the latest news headlines today?", "latest headlines 2026-11-23", "show today's UK news"} {
		if len(publisherNewsFeeds(query)) != 2 {
			t.Fatal(query)
		}
	}
	for _, query := range []string{"latest OpenAI news", "news about a school", "how does a news feed work"} {
		if publisherNewsFeeds(query) != nil {
			t.Fatal(query)
		}
	}
	now := time.Now().UTC()
	rss := fmt.Sprintf(`<rss><channel><item><title>Real &amp; dated</title><link>https://news.example/story#x</link><description>A summary.</description><pubDate>%s</pubDate></item><item><title>Unsafe</title><link>javascript:bad</link><pubDate>%s</pubDate></item></channel></rss>`, now.Format(time.RFC1123Z), now.Format(time.RFC1123Z))
	items, err := parsePublisherFeed([]byte(rss), 5, now)
	if err != nil || len(items) != 1 || items[0].URL != "https://news.example/story" {
		t.Fatalf("items=%v err=%v", items, err)
	}
	for _, body := range []string{rss[:20], strings.ReplaceAll(rss, now.Format(time.RFC1123Z), now.Add(-4*24*time.Hour).Format(time.RFC1123Z)), "<!DOCTYPE rss><rss/>", strings.Repeat("x", publisherFeedLimit+1)} {
		if _, err := parsePublisherFeed([]byte(body), 5, now); err == nil {
			t.Fatal("invalid feed accepted")
		}
	}
}

func TestLivePublisherHeadlines(t *testing.T) {
	if os.Getenv("MAULER_LIVE_NEWS") != "1" {
		t.Skip("explicit read-only publisher feed qualification")
	}
	result, err := publisherHeadlines(context.Background(), publisherNewsFeeds("latest news headlines today"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Article bodies were NOT read") || strings.Count(result, "   URL:") < 3 {
		t.Fatal("missing dated source evidence")
	}
	t.Log("publisher feed live: passed; model calls=0; Telegram messages=0")
}

package tools

// Publisher feeds supplement, rather than replace, Mauler's search pipeline.
// Reading a dated feed is not reading or independently verifying its articles.
import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

const publisherFeedLimit = 1024 * 1024

func publisherNewsFeeds(query string) []string {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	allowed := " what whats s are is the a some get me give find show please latest current top breaking news headline headlines today todays for from in now bbc sky uk british britain world global international technology tech business and of on this morning evening afternoon january february march april may june july august september october november december "
	hasNews := false
	category := ""
	for _, word := range words {
		if word == "news" || word == "headlines" || word == "headline" {
			hasNews = true
		}
		numeric := true
		for _, r := range word {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
		}
		if !numeric && !strings.Contains(allowed, " "+word+" ") {
			return nil
		}
		switch word {
		case "tech", "technology":
			category = "technology"
		case "business":
			category = "business"
		case "uk", "british", "britain":
			if category == "" {
				category = "uk"
			}
		case "world", "global", "international":
			if category == "" {
				category = "world"
			}
		}
	}
	if !hasNews {
		return nil
	}
	if category == "" {
		return []string{"https://feeds.bbci.co.uk/news/rss.xml", "https://feeds.skynews.com/feeds/rss/home.xml"}
	}
	return []string{"https://feeds.bbci.co.uk/news/" + category + "/rss.xml", "https://feeds.skynews.com/feeds/rss/" + category + ".xml"}
}

type publisherItem struct {
	Title     string `xml:"title"`
	URL       string `xml:"link"`
	Summary   string `xml:"description"`
	Published string `xml:"pubDate"`
	date      time.Time
}

func parsePublisherFeed(body []byte, limit int, now time.Time) ([]publisherItem, error) {
	if len(body) > publisherFeedLimit {
		return nil, fmt.Errorf("publisher feed exceeds byte limit")
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("publisher DTD is not supported")
		case xml.StartElement:
			depth++
			if depth > 32 {
				return nil, fmt.Errorf("publisher nesting limit exceeded")
			}
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("incomplete publisher feed")
	}
	var feed struct {
		Channel struct {
			Items []publisherItem `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	var items []publisherItem
	seen := map[string]bool{}
	for _, item := range feed.Channel.Items {
		if len(item.Title) > 64*1024 || len(item.Summary) > 64*1024 {
			return nil, fmt.Errorf("publisher field exceeds limit")
		}
		date, err := time.Parse(time.RFC1123Z, strings.TrimSpace(item.Published))
		if err != nil {
			date, err = time.Parse(time.RFC1123, strings.TrimSpace(item.Published))
		}
		if err != nil {
			date, err = time.Parse(time.RFC3339, strings.TrimSpace(item.Published))
		}
		if err != nil || date.Before(now.Add(-48*time.Hour)) || date.After(now.Add(10*time.Minute)) {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(item.URL))
		if err != nil || u.Host == "" || u.User != nil || len(item.URL) > 2048 || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		u.Fragment = ""
		item.URL = u.String()
		if seen[item.URL] {
			continue
		}
		seen[item.URL] = true
		item.Title = boundedPublisherText(item.Title, 250)
		item.Summary = boundedPublisherText(item.Summary, 600)
		if item.Title == "" {
			continue
		}
		item.date = date.UTC()
		item.Published = item.date.Format(time.RFC3339)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].date.After(items[j].date) })
	if limit < 1 {
		limit = 1
	}
	if limit > 10 {
		limit = 10
	}
	if len(items) > limit {
		items = items[:limit]
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no dated publisher headlines within the last 48 hours")
	}
	return items, nil
}

func boundedPublisherText(text string, limit int) string {
	text = strings.Join(strings.Fields(cleanText(text)), " ")
	runes := []rune(text)
	if len(runes) > limit {
		text = string(runes[:limit])
	}
	return text
}

func publisherHeadlines(ctx context.Context, feeds []string, limit int) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for index, feed := range feeds {
		if index >= 2 {
			break
		}
		result, err := func() (string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Accept", "application/rss+xml,application/xml,text/xml")
			response, err := client.Do(req)
			if err != nil {
				return "", err
			}
			defer response.Body.Close()
			expected, _ := url.Parse(feed)
			if response.StatusCode != 200 || response.Request.URL.Hostname() != expected.Hostname() {
				return "", fmt.Errorf("publisher feed response rejected")
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, publisherFeedLimit+1))
			if err != nil {
				return "", err
			}
			now := time.Now().UTC()
			items, err := parsePublisherFeed(body, limit, now)
			if err != nil {
				return "", err
			}
			var out strings.Builder
			fmt.Fprintf(&out, "# Publisher headlines\nFeed: %s\nRetrieved: %s\nDated entries from the last 48 hours. Use each publication date; do not assume every story happened today. Article bodies were NOT read; these are publisher-reported headlines, not independently verified claims.\n", feed, now.Format(time.RFC3339))
			for i, item := range items {
				fmt.Fprintf(&out, "\n%d. %s\n   Published: %s\n   URL: %s\n   %s\n", i+1, item.Title, item.Published, item.URL, item.Summary)
			}
			return out.String(), nil
		}()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("publisher headlines unavailable: %v", lastErr)
}

package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mauler/internal/settings"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WebSearch queries a configured search backend and returns compact results.
type WebSearch struct{}

func (t *WebSearch) Name() string      { return "web_search" }
func (t *WebSearch) Destructive() bool { return false }

func (t *WebSearch) Description() string {
	return "Search the web and return compact result titles, URLs, and snippets. Supports local SearXNG, DuckDuckGo HTML, and Brave Search when configured."
}

func (t *WebSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "limit": {"type": "integer", "description": "Maximum results to return, default 5, max 10"}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type webSearchParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (t *WebSearch) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p webSearchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("web_search: bad params: %w", err)
	}
	p.Query = strings.TrimSpace(p.Query)
	if p.Query == "" {
		return "", fmt.Errorf("web_search: query is required")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	cfg, _ := settings.Load()
	engine := strings.ToLower(strings.TrimSpace(cfg.Tools.WebEngine))
	switch engine {
	case "", "auto":
		if strings.TrimSpace(cfg.Tools.WebBaseURL) != "" {
			return searchSearXNG(ctx, cfg.Tools.WebBaseURL, p.Query, limit)
		}
		if strings.TrimSpace(cfg.Tools.BraveAPIKey) != "" || strings.TrimSpace(cfg.Tools.WebAPIKeyEnv) != "" {
			return searchBrave(ctx, cfg.Tools.WebAPIKeyEnv, cfg.Tools.BraveAPIKey, p.Query, limit)
		}
		return searchDuckDuckGo(ctx, p.Query, limit)
	case "duckduckgo", "ddg":
		return searchDuckDuckGo(ctx, p.Query, limit)
	case "searxng", "searx":
		return searchSearXNG(ctx, cfg.Tools.WebBaseURL, p.Query, limit)
	case "brave":
		return searchBrave(ctx, cfg.Tools.WebAPIKeyEnv, cfg.Tools.BraveAPIKey, p.Query, limit)
	default:
		return "", fmt.Errorf("web_search: unsupported web engine %q", cfg.Tools.WebEngine)
	}
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

func formatSearchResults(query, engine string, results []searchResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results for %q from %s.", query, engine)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return sourceRank(results[i]) < sourceRank(results[j])
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Search results for %q (%s)\n", query, engine)
	for i, r := range results {
		fmt.Fprintf(&sb, "\n%d. %s\n", i+1, cleanText(r.Title))
		if r.URL != "" {
			fmt.Fprintf(&sb, "   URL: %s\n", r.URL)
		}
		fmt.Fprintf(&sb, "   Source rank: %s\n", sourceRankLabel(r))
		if r.Snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", cleanText(r.Snippet))
		}
	}
	return sb.String()
}

func sourceRank(r searchResult) int {
	u, err := url.Parse(r.URL)
	if err != nil {
		return 90
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	path := strings.ToLower(u.Path)
	title := strings.ToLower(r.Title)
	if strings.HasPrefix(host, "docs.") || strings.Contains(path, "/docs") || strings.Contains(path, "/documentation") {
		return 10
	}
	if strings.HasSuffix(host, ".gov") || strings.HasSuffix(host, ".edu") {
		return 12
	}
	if host == "github.com" || host == "raw.githubusercontent.com" {
		if strings.Contains(path, "/docs") || strings.Contains(path, "readme") || strings.Contains(path, "wiki") {
			return 20
		}
		return 25
	}
	if host == "pkg.go.dev" || host == "npmjs.com" || host == "pypi.org" || host == "docs.rs" || host == "crates.io" || strings.Contains(host, "readthedocs.io") {
		return 30
	}
	if strings.Contains(host, "medium.com") || strings.Contains(host, "dev.to") || strings.Contains(host, "substack.com") || strings.Contains(host, "blog") || strings.Contains(title, "blog") {
		return 60
	}
	if strings.Contains(host, "mirror") || strings.Contains(host, "scrape") {
		return 80
	}
	return 40
}

func sourceRankLabel(r searchResult) string {
	switch rank := sourceRank(r); {
	case rank <= 12:
		return "official docs"
	case rank <= 25:
		return "GitHub/repo docs"
	case rank <= 30:
		return "package docs"
	case rank <= 40:
		return "general source"
	case rank <= 60:
		return "blog/community"
	default:
		return "mirror/low confidence"
	}
}

func searchSearXNG(ctx context.Context, baseURL, query string, limit int) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("web_search: set tools.web_base_url to your SearXNG URL, for example http://localhost:8081")
	}
	u, err := url.Parse(baseURL + "/search")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	var body struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := getJSON(ctx, u.String(), nil, &body); err != nil {
		return "", err
	}
	results := make([]searchResult, 0, limit)
	for _, r := range body.Results {
		if r.URL == "" && r.Title == "" {
			continue
		}
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
		if len(results) >= limit {
			break
		}
	}
	return formatSearchResults(query, "SearXNG", results), nil
}

func searchDuckDuckGo(ctx context.Context, query string, limit int) (string, error) {
	results, err := searchDuckDuckGoHTML(ctx, query, limit)
	if err == nil && len(results) > 0 {
		return formatSearchResults(query, "DuckDuckGo", results), nil
	}

	u, _ := url.Parse("https://api.duckduckgo.com/")
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	q.Set("no_html", "1")
	q.Set("skip_disambig", "1")
	u.RawQuery = q.Encode()

	var body struct {
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Heading       string `json:"Heading"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
			Topics   []struct {
				Text     string `json:"Text"`
				FirstURL string `json:"FirstURL"`
			} `json:"Topics"`
		} `json:"RelatedTopics"`
	}
	if err := getJSON(ctx, u.String(), nil, &body); err != nil {
		return "", err
	}
	results = make([]searchResult, 0, limit)
	if body.AbstractText != "" || body.AbstractURL != "" {
		title := body.Heading
		if title == "" {
			title = query
		}
		results = append(results, searchResult{Title: title, URL: body.AbstractURL, Snippet: body.AbstractText})
	}
	for _, topic := range body.RelatedTopics {
		if topic.FirstURL != "" || topic.Text != "" {
			results = append(results, searchResult{Title: firstSentence(topic.Text), URL: topic.FirstURL, Snippet: topic.Text})
		}
		for _, nested := range topic.Topics {
			results = append(results, searchResult{Title: firstSentence(nested.Text), URL: nested.FirstURL, Snippet: nested.Text})
			if len(results) >= limit {
				break
			}
		}
		if len(results) >= limit {
			break
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return formatSearchResults(query, "DuckDuckGo", results), nil
}

func searchDuckDuckGoHTML(ctx context.Context, query string, limit int) ([]searchResult, error) {
	u, _ := url.Parse("https://html.duckduckgo.com/html/")
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()

	data, err := getBytes(ctx, u.String(), map[string]string{
		"Accept":          "text/html,application/xhtml+xml",
		"Accept-Language": "en-US,en;q=0.9",
	}, 2<<20)
	if err != nil {
		return nil, err
	}
	return parseDuckDuckGoHTML(string(data), limit), nil
}

func searchBrave(ctx context.Context, apiKeyEnv, apiKey, query string, limit int) (string, error) {
	key := strings.TrimSpace(apiKey)
	if apiKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(apiKeyEnv))
	}
	if key == "" {
		return "", fmt.Errorf("web_search: Brave requires tools.web_api_key_env or tools.brave_api_key")
	}
	u, _ := url.Parse("https://api.search.brave.com/res/v1/web/search")
	q := u.Query()
	q.Set("q", query)
	q.Set("count", fmt.Sprintf("%d", limit))
	u.RawQuery = q.Encode()
	headers := map[string]string{
		"Accept":               "application/json",
		"X-Subscription-Token": key,
	}
	var body struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := getJSON(ctx, u.String(), headers, &body); err != nil {
		return "", err
	}
	results := make([]searchResult, 0, limit)
	for _, r := range body.Web.Results {
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
		if len(results) >= limit {
			break
		}
	}
	return formatSearchResults(query, "Brave", results), nil
}

// FetchURL retrieves a web page and returns readable text.
type FetchURL struct{}

func (t *FetchURL) Name() string      { return "fetch_url" }
func (t *FetchURL) Destructive() bool { return false }

func (t *FetchURL) Description() string {
	return "Fetch a URL and return readable text content, truncated to a safe size. Use after web_search when the page itself is needed."
}

func (t *FetchURL) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "HTTP or HTTPS URL to fetch"},
    "max_chars": {"type": "integer", "description": "Maximum characters to return, default 12000, max 30000"}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
}

type fetchURLParams struct {
	URL      string `json:"url"`
	MaxChars int    `json:"max_chars"`
}

func (t *FetchURL) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p fetchURLParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("fetch_url: bad params: %w", err)
	}
	u, err := url.Parse(strings.TrimSpace(p.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("fetch_url: url must be http or https")
	}
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars > 30000 {
		maxChars = 30000
	}
	if text, ok := fetchGitHubReadable(ctx, u, maxChars); ok {
		return fmt.Sprintf("# %s\n\n%s", u.String(), strings.TrimSpace(text)), nil
	}
	text, err := getText(ctx, u.String(), 1<<20)
	if err != nil {
		return "", err
	}
	text = readableHTMLToText(text)
	if len(text) > maxChars {
		text = text[:maxChars] + "\n[truncated]"
	}
	return fmt.Sprintf("# %s\n\n%s", u.String(), strings.TrimSpace(text)), nil
}

func fetchGitHubReadable(ctx context.Context, u *url.URL, maxChars int) (string, bool) {
	if !strings.EqualFold(u.Host, "github.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	owner, repo := parts[0], parts[1]
	if len(parts) >= 5 && (parts[2] == "blob" || parts[2] == "raw") {
		rawURL := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + strings.Join(parts[3:], "/")
		text, err := getText(ctx, rawURL, min(1<<20, maxChars*4))
		if err == nil && strings.TrimSpace(text) != "" {
			return clampReadableText(text, maxChars), true
		}
	}
	var body struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	apiURL := "https://api.github.com/repos/" + owner + "/" + repo + "/readme"
	if err := getJSON(ctx, apiURL, map[string]string{"Accept": "application/vnd.github+json"}, &body); err != nil {
		return "", false
	}
	if body.Encoding != "base64" {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body.Content, "\n", ""))
	if err != nil {
		return "", false
	}
	return clampReadableText("# "+body.Path+"\n\n"+string(decoded), maxChars), true
}

func getJSON(ctx context.Context, target string, headers map[string]string, out interface{}) error {
	data, err := getBytes(ctx, target, headers, 2<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("web_search: decode response: %w", err)
	}
	return nil
}

func getText(ctx context.Context, target string, limit int) (string, error) {
	data, err := getBytes(ctx, target, nil, int64(limit))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func getBytes(ctx context.Context, target string, headers map[string]string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TheMauler/0.1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, target)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

var (
	scriptRE     = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	ddgResultRE  = regexp.MustCompile(`(?is)<div[^>]*class="[^"]*\bresult\b[^"]*"[^>]*>(.*?)</div>\s*</div>`)
	ddgLinkRE    = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*\bresult__a\b[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE = regexp.MustCompile(`(?is)<a[^>]*class="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</a>|<div[^>]*class="[^"]*\bresult__snippet\b[^"]*"[^>]*>(.*?)</div>`)
	tagRE        = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRE      = regexp.MustCompile(`[ \t\r\f\v]+`)
	blankRE      = regexp.MustCompile(`\n{3,}`)
)

func parseDuckDuckGoHTML(page string, limit int) []searchResult {
	matches := ddgResultRE.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		matches = [][]string{{"", page}}
	}
	results := make([]searchResult, 0, limit)
	seen := map[string]bool{}
	for _, match := range matches {
		block := match[1]
		link := ddgLinkRE.FindStringSubmatch(block)
		if len(link) < 3 {
			continue
		}
		resultURL := decodeDuckDuckGoURL(link[1])
		title := cleanHTMLText(link[2])
		if title == "" || resultURL == "" || seen[resultURL] {
			continue
		}
		snippet := ""
		if snippetMatch := ddgSnippetRE.FindStringSubmatch(block); len(snippetMatch) > 0 {
			for _, part := range snippetMatch[1:] {
				if strings.TrimSpace(part) != "" {
					snippet = cleanHTMLText(part)
					break
				}
			}
		}
		results = append(results, searchResult{Title: title, URL: resultURL, Snippet: snippet})
		seen[resultURL] = true
		if len(results) >= limit {
			break
		}
	}
	return results
}

func decodeDuckDuckGoURL(raw string) string {
	raw = html.UnescapeString(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err == nil && strings.Contains(u.Host, "duckduckgo.com") {
		if uddg := u.Query().Get("uddg"); uddg != "" {
			if decoded, err := url.QueryUnescape(uddg); err == nil {
				return decoded
			}
			return uddg
		}
	}
	return raw
}

func cleanHTMLText(s string) string {
	s = tagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return cleanText(s)
}

func htmlToText(s string) string {
	s = scriptRE.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = tagRE.ReplaceAllString(s, " ")
	s = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(spaceRE.ReplaceAllString(line, " "))
	}
	s = strings.Join(lines, "\n")
	return strings.TrimSpace(blankRE.ReplaceAllString(s, "\n\n"))
}

func readableHTMLToText(s string) string {
	s = stripHTMLComments(s)
	s = scriptRE.ReplaceAllString(s, " ")
	for _, tag := range []string{"article", "main", "body"} {
		if section := largestTagText(s, tag); len(section) > 40 {
			return section
		}
	}
	return htmlToText(s)
}

func largestTagText(page, tag string) string {
	re := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>(.*?)</` + tag + `>`)
	matches := re.FindAllStringSubmatch(page, -1)
	best := ""
	for _, match := range matches {
		text := htmlToText(match[1])
		if len(text) > len(best) {
			best = text
		}
	}
	return best
}

func stripHTMLComments(s string) string {
	commentRE := regexp.MustCompile(`(?is)<!--.*?-->`)
	return commentRE.ReplaceAllString(s, " ")
}

func clampReadableText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars > 0 && len(text) > maxChars {
		return text[:maxChars] + "\n[truncated]"
	}
	return text
}

func cleanText(s string) string {
	return strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
}

func firstSentence(s string) string {
	s = cleanText(s)
	if i := strings.Index(s, ". "); i > 0 && i < 90 {
		return s[:i+1]
	}
	if len(s) > 90 {
		return s[:90] + "..."
	}
	return s
}

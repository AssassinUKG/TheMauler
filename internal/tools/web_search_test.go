package tools

import (
	"strings"
	"testing"
)

func TestSourceRankOrdering(t *testing.T) {
	cases := []struct {
		name string
		url  string
		max  int
	}{
		{name: "official docs", url: "https://docs.example.com/guide", max: 12},
		{name: "github docs", url: "https://github.com/acme/project/tree/main/docs/install.md", max: 25},
		{name: "package docs", url: "https://pkg.go.dev/example.com/acme/project", max: 30},
		{name: "blog", url: "https://dev.to/acme/installing-project", max: 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sourceRank(searchResult{Title: tc.name, URL: tc.url})
			if got > tc.max {
				t.Fatalf("sourceRank() = %d, want <= %d", got, tc.max)
			}
		})
	}
}

func TestFormatSearchResultsIncludesRank(t *testing.T) {
	out := formatSearchResults("x", "test", []searchResult{{Title: "Docs", URL: "https://docs.example.com/x"}})
	if !strings.Contains(out, "Source rank: official docs") {
		t.Fatalf("formatted output missing source rank: %s", out)
	}
}

func TestReadableHTMLToTextPrefersMainContent(t *testing.T) {
	page := `<html><head><style>.x{display:none}</style><script>alert(1)</script></head><body><nav>Home Pricing Login</nav><main><h1>Exploit Notes</h1><p>Use the endpoint with the token parameter.</p></main><footer>Footer noise</footer></body></html>`
	got := readableHTMLToText(page)
	if !strings.Contains(got, "Exploit Notes") || !strings.Contains(got, "token parameter") {
		t.Fatalf("missing main content: %q", got)
	}
	if strings.Contains(got, "display:none") || strings.Contains(got, "alert(1)") || strings.Contains(got, "Home Pricing") {
		t.Fatalf("included noisy page chrome: %q", got)
	}
}

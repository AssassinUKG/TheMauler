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

package hackernews

import (
	"strings"
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

func TestFormatPathID(t *testing.T) {
	comments := []itemNode{
		{
			Author: "alice",
			Text:   `<p>Main point with <a href="https://example.com">link</a> and [1] ref.`,
			Children: []itemNode{
				{Author: "bob", Text: "<p>Reply<br>continued"},
			},
		},
		{
			Author: "",
			Text:   "",
		},
	}

	got := FormatPathID(comments)
	want := strings.Join([]string{
		"[1] alice: Main point with link and {1} ref.",
		"",
		"[1.1] bob: Reply continued",
		"",
		"[2] Anonymous: (deleted)",
	}, "\n")

	if got != want {
		t.Fatalf("unexpected Path ID output:\n%s", got)
	}
}

func TestCountComments(t *testing.T) {
	comments := []itemNode{
		{Children: []itemNode{{}, {Children: []itemNode{{}}}}},
		{},
	}
	if got := CountComments(comments); got != 5 {
		t.Fatalf("CountComments() = %d, want 5", got)
	}
}

func TestNormalizeArticleURL(t *testing.T) {
	got := normalizeArticleURL("https://www.example.com/post/?utm_source=rss&b=2&a=1#atom-everything")
	want := "example.com/post?a=1&b=2"
	if got != want {
		t.Fatalf("normalizeArticleURL() = %q, want %q", got, want)
	}
}

func TestEnrichArticleUsesCachedHNSummary(t *testing.T) {
	result, err := EnrichArticle(model.Article{
		HNItemID:   42,
		HNPoints:   10,
		HNComments: 3,
		HNSummary:  "cached HN summary",
	}, summarizer.Options{}, false)
	if err != nil {
		t.Fatalf("enrich article: %v", err)
	}
	if result == nil || !result.Cached {
		t.Fatalf("expected cached result, got %+v", result)
	}
	if result.URL != "https://news.ycombinator.com/item?id=42" {
		t.Fatalf("expected reconstructed HN URL, got %q", result.URL)
	}
	if result.DiscussionSummary != "cached HN summary" {
		t.Fatalf("expected cached summary, got %q", result.DiscussionSummary)
	}
}

func TestEnrichArticleUsesCachedZeroCommentItem(t *testing.T) {
	fetched := time.Now()
	result, err := EnrichArticle(model.Article{
		HNItemID:   42,
		HNPoints:   10,
		HNComments: 0,
		HNFetched:  &fetched,
	}, summarizer.Options{}, false)
	if err != nil {
		t.Fatalf("enrich article: %v", err)
	}
	if result == nil || !result.Cached {
		t.Fatalf("expected cached result, got %+v", result)
	}
	if result.Comments != 0 {
		t.Fatalf("expected zero comments, got %d", result.Comments)
	}
}

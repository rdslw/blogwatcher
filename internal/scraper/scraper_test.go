package scraper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestScrapeBlog(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<body>
  <article><h2><a href="/one">First</a></h2></article>
  <article class="news-card">
    <a href="/two">
      <span class="eyebrow">Mar 12, 2026</span>
      <span class="category">Announcements</span>
      <span class="post-title">Anthropic invests in research</span>
    </a>
  </article>
  <article><h2><a href="/one">First Duplicate</a></h2></article>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	articles, err := ScrapeBlog(server.URL, "article h2 a, .news-card a", 2*time.Second)
	if err != nil {
		t.Fatalf("scrape blog: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}
	if articles[0].Title != "First" {
		t.Fatalf("expected first title %q, got %q", "First", articles[0].Title)
	}
	if articles[1].Title != "Anthropic invests in research" {
		t.Fatalf("expected second title %q, got %q", "Anthropic invests in research", articles[1].Title)
	}
	if articles[0].URL == "" || articles[1].URL == "" {
		t.Fatalf("expected URLs")
	}
}

func TestExtractTitlePrefersTitleClassSegment(t *testing.T) {
	link, parent := mustSelections(t, `
<article class="news-card">
  <a href="/two">
    <span class="eyebrow">Mar 12, 2026</span>
    <span class="category">Announcements</span>
    <span class="post-title">Anthropic invests in research</span>
  </a>
</article>`)

	title := extractTitle(link, parent)
	if title != "Anthropic invests in research" {
		t.Fatalf("expected structured title, got %q", title)
	}
}

func TestExtractTitleDoesNotMistakeSubtitleForTitle(t *testing.T) {
	link, parent := mustSelections(t, `
<article>
  <a href="/two" title="Actual title">
    <span class="subtitle">Ignore me</span>
  </a>
</article>`)

	title := extractTitle(link, parent)
	if title != "Actual title" {
		t.Fatalf("expected title attribute fallback, got %q", title)
	}
}

func TestExtractTitleDoesNotUseParentHeading(t *testing.T) {
	link, parent := mustSelections(t, `
<article>
  <h2>Container heading</h2>
  <a href="/two" title="Link title">
    <span class="meta">Mar 12, 2026</span>
  </a>
</article>`)

	title := extractTitle(link, parent)
	if title != "Link title" {
		t.Fatalf("expected link-scoped title, got %q", title)
	}
}

func mustSelections(t *testing.T, html string) (*goquery.Selection, *goquery.Selection) {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse test html: %v", err)
	}

	parent := doc.Find("article").First()
	link := parent.Find("a").First()
	if parent.Length() == 0 || link.Length() == 0 {
		t.Fatalf("expected article and link in test html")
	}

	return link, parent
}

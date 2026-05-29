package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/controller"
	"github.com/rdslw/blogwatcher/internal/hackernews"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/storage"
)

func ptrTime(t time.Time) *time.Time { return &t }

func TestToJSONBlog(t *testing.T) {
	scanned := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	b := model.Blog{
		ID: 7, Name: "Tech", URL: "https://t.example", FeedURL: "https://t.example/rss",
		ScrapeSelector: "article a", LastScanned: &scanned,
	}
	stats := storage.ArticleStats{Total: 10, Unread: 3, Hide: 1, Normal: 1, Prefer: 1}

	got := toJSONBlog(b, stats)
	if got.ID != 7 || got.Name != "Tech" || got.URL != "https://t.example" {
		t.Fatalf("blog fields wrong: %+v", got)
	}
	if got.FeedURL != "https://t.example/rss" || got.ScrapeSelector != "article a" {
		t.Fatalf("feed/selector wrong: %+v", got)
	}
	if got.LastScanned == nil || !got.LastScanned.Equal(scanned) {
		t.Fatalf("last_scanned wrong: %v", got.LastScanned)
	}
	if got.Stats != (jsonArticleStat{Total: 10, Unread: 3, Hide: 1, Normal: 1, Prefer: 1}) {
		t.Fatalf("stats wrong: %+v", got.Stats)
	}

	// optional fields omit when empty
	bare := toJSONBlog(model.Blog{ID: 1, Name: "x", URL: "u"}, storage.ArticleStats{})
	data, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	js := string(data)
	for _, banned := range []string{`"feed_url"`, `"scrape_selector"`, `"last_scanned"`} {
		if contains(js, banned) {
			t.Fatalf("expected %s omitted, got %s", banned, js)
		}
	}
}

func TestToJSONArticleHN(t *testing.T) {
	fetched := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	a := model.Article{
		ID: 42, BlogID: 7, Title: "T", URL: "https://x/y#atom-everything",
		HNItemID: 1234, HNPoints: 5, HNComments: 10, HNSummary: "discussion", HNFetched: &fetched,
	}
	out := toJSONArticle(a, "Tech")
	if out.URL != "https://x/y" {
		t.Fatalf("URL not normalized: %s", out.URL)
	}
	if out.BlogName != "Tech" {
		t.Fatalf("blog name not set: %+v", out)
	}
	if out.HN == nil {
		t.Fatalf("expected HN populated")
	}
	if !out.HN.Found || out.HN.ItemID != 1234 || out.HN.Points != 5 || out.HN.Comments != 10 {
		t.Fatalf("HN fields wrong: %+v", out.HN)
	}
	if out.HN.URL != "https://news.ycombinator.com/item?id=1234" {
		t.Fatalf("HN URL wrong: %s", out.HN.URL)
	}

	// checked but no match
	notFound := model.Article{ID: 1, HNFetched: &fetched, HNItemID: 0}
	out2 := toJSONArticle(notFound, "")
	if out2.HN == nil || out2.HN.Found || out2.HN.ItemID != 0 {
		t.Fatalf("not-found HN wrong: %+v", out2.HN)
	}

	// never checked: HN omitted
	never := model.Article{ID: 1}
	out3 := toJSONArticle(never, "")
	if out3.HN != nil {
		t.Fatalf("HN should be nil when never checked: %+v", out3.HN)
	}
}

func TestToJSONHNResult(t *testing.T) {
	if toJSONHNResult(nil) != nil {
		t.Fatal("nil input must produce nil")
	}
	nf := toJSONHNResult(&hackernews.Result{NotFound: true, Cached: true})
	if nf == nil || nf.Found || !nf.Cached {
		t.Fatalf("not-found mapping wrong: %+v", nf)
	}
	ok := toJSONHNResult(&hackernews.Result{
		ID: 99, URL: "https://news.ycombinator.com/item?id=99",
		Points: 12, Comments: 3, DiscussionSummary: "s", Warning: "w", Cached: false,
	})
	if !ok.Found || ok.ItemID != 99 || ok.Summary != "s" || ok.Warning != "w" {
		t.Fatalf("hn mapping wrong: %+v", ok)
	}
}

func TestToJSONSummaryAndInterestResults(t *testing.T) {
	a := model.Article{ID: 1, Title: "T", URL: "u"}
	sr := controller.SummaryResult{
		Article: a, BlogName: "B", Engine: "openai", Cached: true, Warning: "w",
		HackerNews: &hackernews.Result{ID: 7, URL: "h", Points: 1, Comments: 2, Cached: true},
	}
	js := toJSONSummaryResult(sr)
	if js.Engine != "openai" || !js.Cached || js.BlogName != "B" || js.Warning != "w" {
		t.Fatalf("summary mapping wrong: %+v", js)
	}
	if js.HN == nil || js.HN.ItemID != 7 {
		t.Fatalf("summary HN missing: %+v", js.HN)
	}

	ir := controller.InterestResult{
		Article: a, BlogName: "B", Engine: "openai", Skipped: true, Note: "n",
	}
	ji := toJSONInterestResult(ir)
	if !ji.Skipped || ji.Note != "n" || ji.HN != nil {
		t.Fatalf("interest mapping wrong: %+v", ji)
	}

	// roundtrip the outer envelope to ensure it parses as JSON.
	doc := struct {
		Interests []jsonInterestResult `json:"interests"`
	}{Interests: []jsonInterestResult{ji}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back struct {
		Interests []jsonInterestResult `json:"interests"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Interests) != 1 || back.Interests[0].Article.ID != 1 {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
}

// contains is a tiny strings.Contains used to keep imports light.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

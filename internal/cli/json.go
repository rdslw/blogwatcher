package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/rdslw/blogwatcher/internal/controller"
	"github.com/rdslw/blogwatcher/internal/hackernews"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/storage"
)

// JSON DTOs for `--json` output. Field names are stable and intended for
// agentic consumers. Optional fields are omitted when empty/zero so payloads
// stay compact and easy to scan.

type jsonBlog struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	URL            string          `json:"url"`
	FeedURL        string          `json:"feed_url,omitempty"`
	ScrapeSelector string          `json:"scrape_selector,omitempty"`
	LastScanned    *time.Time      `json:"last_scanned,omitempty"`
	Stats          jsonArticleStat `json:"stats"`
}

type jsonArticleStat struct {
	Total  int `json:"total"`
	Unread int `json:"unread"`
	Hide   int `json:"hide"`
	Normal int `json:"normal"`
	Prefer int `json:"prefer"`
}

type jsonArticle struct {
	ID             int64      `json:"id"`
	BlogID         int64      `json:"blog_id"`
	BlogName       string     `json:"blog_name,omitempty"`
	Title          string     `json:"title"`
	URL            string     `json:"url"`
	PublishedDate  *time.Time `json:"published_date,omitempty"`
	DiscoveredDate *time.Time `json:"discovered_date,omitempty"`
	IsRead         bool       `json:"is_read"`
	Summary        string     `json:"summary,omitempty"`
	SummaryEngine  string     `json:"summary_engine,omitempty"`
	InterestState  string     `json:"interest_state,omitempty"`
	InterestReason string     `json:"interest_reason,omitempty"`
	InterestEngine string     `json:"interest_engine,omitempty"`
	InterestJudged *time.Time `json:"interest_judged,omitempty"`
	HN             *jsonHN    `json:"hn,omitempty"`
}

type jsonHN struct {
	Fetched  *time.Time `json:"fetched,omitempty"`
	Found    bool       `json:"found"`
	ItemID   int64      `json:"item_id,omitempty"`
	URL      string     `json:"url,omitempty"`
	Points   int        `json:"points,omitempty"`
	Comments int        `json:"comments,omitempty"`
	Summary  string     `json:"summary,omitempty"`
	Warning  string     `json:"warning,omitempty"`
	Cached   bool       `json:"cached,omitempty"`
}

type jsonSummaryResult struct {
	Article  jsonArticle `json:"article"`
	BlogName string      `json:"blog_name,omitempty"`
	Engine   string      `json:"engine,omitempty"`
	Cached   bool        `json:"cached,omitempty"`
	Upgraded bool        `json:"upgraded,omitempty"`
	Warning  string      `json:"warning,omitempty"`
	HN       *jsonHN     `json:"hn,omitempty"`
}

type jsonInterestResult struct {
	Article  jsonArticle `json:"article"`
	BlogName string      `json:"blog_name,omitempty"`
	Engine   string      `json:"engine,omitempty"`
	Cached   bool        `json:"cached,omitempty"`
	Skipped  bool        `json:"skipped,omitempty"`
	Note     string      `json:"note,omitempty"`
	HN       *jsonHN     `json:"hn,omitempty"`
}

func toJSONBlog(b model.Blog, stats storage.ArticleStats) jsonBlog {
	return jsonBlog{
		ID:             b.ID,
		Name:           b.Name,
		URL:            b.URL,
		FeedURL:        b.FeedURL,
		ScrapeSelector: b.ScrapeSelector,
		LastScanned:    b.LastScanned,
		Stats: jsonArticleStat{
			Total:  stats.Total,
			Unread: stats.Unread,
			Hide:   stats.Hide,
			Normal: stats.Normal,
			Prefer: stats.Prefer,
		},
	}
}

// toJSONArticle converts a model.Article. The blogName may be empty.
// HN data is included only when the article has been checked (HNFetched != nil).
func toJSONArticle(a model.Article, blogName string) jsonArticle {
	out := jsonArticle{
		ID:             a.ID,
		BlogID:         a.BlogID,
		BlogName:       blogName,
		Title:          a.Title,
		URL:            displayArticleURL(a.URL),
		PublishedDate:  a.PublishedDate,
		DiscoveredDate: a.DiscoveredDate,
		IsRead:         a.IsRead,
		Summary:        a.Summary,
		SummaryEngine:  a.SummaryEngine,
		InterestState:  a.InterestState,
		InterestReason: a.InterestReason,
		InterestEngine: a.InterestEngine,
		InterestJudged: a.InterestJudged,
	}
	if a.HNFetched != nil {
		hn := jsonHN{Fetched: a.HNFetched, Found: a.HNItemID > 0}
		if a.HNItemID > 0 {
			hn.ItemID = a.HNItemID
			hn.URL = hackernews.ItemURL(a.HNItemID)
			hn.Points = a.HNPoints
			hn.Comments = a.HNComments
			hn.Summary = a.HNSummary
		}
		out.HN = &hn
	}
	return out
}

// toJSONHNResult mirrors the live *hackernews.Result attached by summary/interest
// runs. Returns nil when the input is nil.
func toJSONHNResult(r *hackernews.Result) *jsonHN {
	if r == nil {
		return nil
	}
	if r.NotFound {
		return &jsonHN{Found: false, Cached: r.Cached}
	}
	return &jsonHN{
		Found:    true,
		ItemID:   r.ID,
		URL:      r.URL,
		Points:   r.Points,
		Comments: r.Comments,
		Summary:  r.DiscussionSummary,
		Warning:  r.Warning,
		Cached:   r.Cached,
	}
}

func toJSONSummaryResult(r controller.SummaryResult) jsonSummaryResult {
	return jsonSummaryResult{
		Article:  toJSONArticle(r.Article, r.BlogName),
		BlogName: r.BlogName,
		Engine:   r.Engine,
		Cached:   r.Cached,
		Upgraded: r.Upgraded,
		Warning:  r.Warning,
		HN:       toJSONHNResult(r.HackerNews),
	}
}

func toJSONInterestResult(r controller.InterestResult) jsonInterestResult {
	return jsonInterestResult{
		Article:  toJSONArticle(r.Article, r.BlogName),
		BlogName: r.BlogName,
		Engine:   r.Engine,
		Cached:   r.Cached,
		Skipped:  r.Skipped,
		Note:     r.Note,
		HN:       toJSONHNResult(r.HackerNews),
	}
}

// emitJSON writes the value to stdout as indented JSON followed by a newline.
func emitJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("emit json: %w", err)
	}
	return nil
}

// emitJSONError writes {"error": "..."} to stdout and returns a markError so the
// caller exits non-zero without the default text error being printed again.
func emitJSONError(err error) error {
	_ = emitJSON(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
	return markError(err)
}

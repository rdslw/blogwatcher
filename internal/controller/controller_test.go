package controller

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/hackernews"
	"github.com/rdslw/blogwatcher/internal/interest"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/storage"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

func TestAddBlogAndRemoveBlog(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}

	if _, err := AddBlog(db, "Test", "https://other.com", "", ""); err == nil {
		t.Fatalf("expected duplicate name error")
	}

	if _, err := AddBlog(db, "Other", "https://example.com", "", ""); err == nil {
		t.Fatalf("expected duplicate url error")
	}

	if err := RemoveBlog(db, blog.Name); err != nil {
		t.Fatalf("remove blog: %v", err)
	}
}

func TestArticleReadUnread(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	read, err := MarkArticleRead(db, article.ID)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if read.IsRead {
		t.Fatalf("expected original state unread")
	}

	unread, err := MarkArticleUnread(db, article.ID)
	if err != nil {
		t.Fatalf("mark unread: %v", err)
	}
	if !unread.IsRead {
		t.Fatalf("expected original state read")
	}
}

func articleIDs(articles []model.Article) []int64 {
	ids := make([]int64, len(articles))
	for i, article := range articles {
		ids[i] = article.ID
	}
	return ids
}

func equalArticleIDSet(articles []model.Article, want []int64) bool {
	if len(articles) != len(want) {
		return false
	}
	gotSet := make(map[int64]struct{}, len(articles))
	for _, article := range articles {
		gotSet[article.ID] = struct{}{}
	}
	for _, id := range want {
		if _, ok := gotSet[id]; !ok {
			return false
		}
	}
	return true
}

func TestGetArticlesFilters(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	_, err = db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	articles, blogNames, err := GetArticles(db, false, "", "all")
	if err != nil {
		t.Fatalf("get articles: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected article")
	}
	if blogNames[blog.ID] != blog.Name {
		t.Fatalf("expected blog name")
	}

	if _, _, err := GetArticles(db, false, "Missing", "all"); err == nil {
		t.Fatalf("expected blog not found error")
	}
}

func TestGetArticlesInterestFilter(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	for _, tc := range []struct {
		title string
		state string
	}{
		{"Preferred", model.InterestStatePrefer},
		{"Normal", model.InterestStateNormal},
		{"Hidden", model.InterestStateHide},
		{"Unclassified", ""},
	} {
		a, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: tc.title, URL: "https://example.com/" + tc.title})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if tc.state != "" {
			if err := db.UpdateArticleInterest(a.ID, tc.state, "test", "test", time.Now()); err != nil {
				t.Fatalf("update interest: %v", err)
			}
		}
	}

	all, _, err := GetArticles(db, true, "", "all")
	if err != nil {
		t.Fatalf("filter all: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 articles with filter=all, got %d", len(all))
	}

	norm, _, err := GetArticles(db, true, "", "norm")
	if err != nil {
		t.Fatalf("filter norm: %v", err)
	}
	if len(norm) != 2 {
		t.Fatalf("expected 2 articles with filter=norm, got %d", len(norm))
	}
	for _, a := range norm {
		if a.InterestState != model.InterestStateNormal && a.InterestState != "" {
			t.Fatalf("filter=norm should include only normal and unclassified articles, got %q", a.InterestState)
		}
	}

	prefer, _, err := GetArticles(db, true, "", "prefer")
	if err != nil {
		t.Fatalf("filter prefer: %v", err)
	}
	if len(prefer) != 1 {
		t.Fatalf("expected 1 article with filter=prefer, got %d", len(prefer))
	}
	if prefer[0].InterestState != model.InterestStatePrefer {
		t.Fatalf("expected prefer state, got %q", prefer[0].InterestState)
	}

	prefFilter, err := ParseInterestFilter([]string{"pref"})
	if err != nil {
		t.Fatalf("parse pref: %v", err)
	}
	pref, _, err := GetArticlesByFilter(db, true, "", prefFilter)
	if err != nil {
		t.Fatalf("filter pref: %v", err)
	}
	if len(pref) != 1 || pref[0].InterestState != model.InterestStatePrefer {
		t.Fatalf("expected pref alias to match prefer, got %+v", pref)
	}

	combinedFilter, err := ParseInterestFilter([]string{"hide,normal", "pref"})
	if err != nil {
		t.Fatalf("parse combined filter: %v", err)
	}
	combined, _, err := GetArticlesByFilter(db, true, "", combinedFilter)
	if err != nil {
		t.Fatalf("filter combined: %v", err)
	}
	if len(combined) != 4 {
		t.Fatalf("expected combined filter to include all 4 articles, got %d", len(combined))
	}

	if _, err := ParseInterestFilter([]string{"unknown"}); err == nil {
		t.Fatalf("expected invalid filter error")
	}
	if _, err := ParseInterestFilter([]string{"all,pref"}); err == nil {
		t.Fatalf("expected all plus specific filter error")
	}
}

func TestParseSince(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	now := time.Date(2026, 5, 30, 15, 45, 0, 0, loc)

	cases := []struct {
		value string
		want  time.Time
	}{
		{"2026-05-01", time.Date(2026, 5, 1, 0, 0, 0, 0, loc)},
		{"7", time.Date(2026, 5, 23, 0, 0, 0, 0, loc)},
		{"0", time.Date(2026, 5, 30, 0, 0, 0, 0, loc)},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			got, err := ParseSince(tc.value, now)
			if err != nil {
				t.Fatalf("ParseSince(%q): %v", tc.value, err)
			}
			if got == nil || !got.Equal(tc.want) {
				t.Fatalf("ParseSince(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}

	if got, err := ParseSince("", now); err != nil || got != nil {
		t.Fatalf("ParseSince empty = %v, %v; want nil, nil", got, err)
	}
	if _, err := ParseSince("not-a-date", now); err == nil {
		t.Fatalf("expected invalid string error")
	}
	if _, err := ParseSince("-1", now); err == nil {
		t.Fatalf("expected negative days error")
	}
}

func TestGetArticlesByFilterSince(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	otherBlog, err := AddBlog(db, "Other", "https://other.example.com", "", "")
	if err != nil {
		t.Fatalf("add other blog: %v", err)
	}

	oldPublished := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	newPublished := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	newDiscovered := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)

	oldWithNewDiscovery, err := db.AddArticle(model.Article{
		BlogID:         blog.ID,
		Title:          "Old published with new discovery",
		URL:            "https://example.com/old-published",
		PublishedDate:  &oldPublished,
		DiscoveredDate: &newDiscovered,
	})
	if err != nil {
		t.Fatalf("add old published: %v", err)
	}
	cutoffArticle, err := db.AddArticle(model.Article{
		BlogID:        blog.ID,
		Title:         "Cutoff",
		URL:           "https://example.com/cutoff",
		PublishedDate: &cutoff,
	})
	if err != nil {
		t.Fatalf("add cutoff article: %v", err)
	}
	newArticle, err := db.AddArticle(model.Article{
		BlogID:        blog.ID,
		Title:         "New",
		URL:           "https://example.com/new",
		PublishedDate: &newPublished,
	})
	if err != nil {
		t.Fatalf("add new article: %v", err)
	}
	fallbackArticle, err := db.AddArticle(model.Article{
		BlogID:         blog.ID,
		Title:          "Fallback",
		URL:            "https://example.com/fallback",
		DiscoveredDate: &newDiscovered,
	})
	if err != nil {
		t.Fatalf("add fallback article: %v", err)
	}
	noDate, err := db.AddArticle(model.Article{
		BlogID: blog.ID,
		Title:  "No date",
		URL:    "https://example.com/no-date",
	})
	if err != nil {
		t.Fatalf("add no-date article: %v", err)
	}
	other, err := db.AddArticle(model.Article{
		BlogID:        otherBlog.ID,
		Title:         "Other",
		URL:           "https://other.example.com/new",
		PublishedDate: &newPublished,
	})
	if err != nil {
		t.Fatalf("add other article: %v", err)
	}

	for _, article := range []model.Article{cutoffArticle, newArticle, fallbackArticle, oldWithNewDiscovery, noDate, other} {
		if err := db.UpdateArticleInterest(article.ID, model.InterestStatePrefer, "test", "test", time.Now()); err != nil {
			t.Fatalf("update interest for %d: %v", article.ID, err)
		}
	}
	if _, err := db.MarkArticleRead(newArticle.ID); err != nil {
		t.Fatalf("mark new article read: %v", err)
	}

	filter, err := ParseInterestFilter([]string{"pref"})
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}

	unread, _, err := GetArticlesByFilterSince(db, false, "Test", filter, &cutoff)
	if err != nil {
		t.Fatalf("get unread since: %v", err)
	}
	if !equalArticleIDSet(unread, []int64{cutoffArticle.ID, fallbackArticle.ID}) {
		t.Fatalf("unread since got IDs %v", articleIDs(unread))
	}

	all, _, err := GetArticlesByFilterSince(db, true, "Test", filter, &cutoff)
	if err != nil {
		t.Fatalf("get all since: %v", err)
	}
	if !equalArticleIDSet(all, []int64{cutoffArticle.ID, newArticle.ID, fallbackArticle.ID}) {
		t.Fatalf("all since got IDs %v", articleIDs(all))
	}
}

func TestMarkArticlesReadByFilter(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	for _, tc := range []struct {
		title string
		state string
	}{
		{"Preferred", model.InterestStatePrefer},
		{"Normal", model.InterestStateNormal},
		{"Hidden", model.InterestStateHide},
		{"Unclassified", ""},
	} {
		a, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: tc.title, URL: "https://example.com/" + tc.title})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if tc.state != "" {
			if err := db.UpdateArticleInterest(a.ID, tc.state, "test", "test", time.Now()); err != nil {
				t.Fatalf("update interest: %v", err)
			}
		}
	}

	hideFilter, err := ParseInterestFilter([]string{"hide"})
	if err != nil {
		t.Fatalf("parse hide filter: %v", err)
	}
	marked, err := MarkArticlesReadByFilter(db, "", hideFilter)
	if err != nil {
		t.Fatalf("filter hide: %v", err)
	}
	if len(marked) != 1 || marked[0].Title != "Hidden" {
		t.Fatalf("expected 1 hidden article marked, got %d", len(marked))
	}

	preferFilter, err := ParseInterestFilter([]string{"pref"})
	if err != nil {
		t.Fatalf("parse prefer filter: %v", err)
	}
	marked, err = MarkArticlesReadByFilter(db, "", preferFilter)
	if err != nil {
		t.Fatalf("filter prefer: %v", err)
	}
	if len(marked) != 1 || marked[0].Title != "Preferred" {
		t.Fatalf("expected 1 preferred article marked, got %d", len(marked))
	}

	allFilter, err := ParseInterestFilter([]string{"all"})
	if err != nil {
		t.Fatalf("parse all filter: %v", err)
	}
	marked, err = MarkArticlesReadByFilter(db, "", allFilter)
	if err != nil {
		t.Fatalf("filter all: %v", err)
	}
	if len(marked) != 2 {
		t.Fatalf("expected 2 remaining articles marked, got %d", len(marked))
	}
}

func TestMarkArticlesReadByFilterWithBlog(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog1, err := AddBlog(db, "Blog1", "https://blog1.example.com", "", "")
	if err != nil {
		t.Fatalf("add blog1: %v", err)
	}
	blog2, err := AddBlog(db, "Blog2", "https://blog2.example.com", "", "")
	if err != nil {
		t.Fatalf("add blog2: %v", err)
	}

	for _, blogID := range []int64{blog1.ID, blog2.ID} {
		a, err := db.AddArticle(model.Article{BlogID: blogID, Title: "Art", URL: fmt.Sprintf("https://example.com/%d", blogID)})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if err := db.UpdateArticleInterest(a.ID, model.InterestStateHide, "test", "test", time.Now()); err != nil {
			t.Fatalf("update interest: %v", err)
		}
	}

	hideFilter, err := ParseInterestFilter([]string{"hide"})
	if err != nil {
		t.Fatalf("parse hide filter: %v", err)
	}
	marked, err := MarkArticlesReadByFilter(db, "Blog1", hideFilter)
	if err != nil {
		t.Fatalf("filter hide blog1: %v", err)
	}
	if len(marked) != 1 {
		t.Fatalf("expected 1 article from Blog1, got %d", len(marked))
	}

	// Blog2 article should still be unread
	remaining, _, err := GetArticles(db, false, "Blog2", "all")
	if err != nil {
		t.Fatalf("get blog2 articles: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected Blog2 article still unread, got %d", len(remaining))
	}
}

func TestSummarizeArticlesByFilterAppliesBeforeLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	preferred, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Preferred", URL: "https://example.com/preferred"})
	if err != nil {
		t.Fatalf("add preferred article: %v", err)
	}
	hidden, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Hidden", URL: "https://example.com/hidden"})
	if err != nil {
		t.Fatalf("add hidden article: %v", err)
	}
	if err := db.UpdateArticleInterest(preferred.ID, model.InterestStatePrefer, "test", "test", time.Now()); err != nil {
		t.Fatalf("update preferred interest: %v", err)
	}
	if err := db.UpdateArticleInterest(hidden.ID, model.InterestStateHide, "test", "test", time.Now()); err != nil {
		t.Fatalf("update hidden interest: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	var summarized []string
	summarizeArticleFn = func(url string, forceExtractive bool, opts summarizer.Options) (summarizer.Result, error) {
		summarized = append(summarized, url)
		return summarizer.Result{Summary: "summary", Engine: summarizer.EngineSnippet}, nil
	}

	filter, err := ParseInterestFilter([]string{"pref"})
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	results, err := SummarizeArticlesByFilter(db, false, "", filter, false, false, 1, 1, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize filtered articles: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != preferred.ID {
		t.Fatalf("expected only preferred article, got %+v", results)
	}
	if len(summarized) != 1 || summarized[0] != preferred.URL {
		t.Fatalf("expected only preferred URL summarized, got %v", summarized)
	}
}

func TestSummarizeArticlesSinceAppliesBeforeLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	oldDate := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	newDate := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	oldArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Old", URL: "https://example.com/old", PublishedDate: &oldDate})
	if err != nil {
		t.Fatalf("add old article: %v", err)
	}
	newArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "New", URL: "https://example.com/new", PublishedDate: &newDate})
	if err != nil {
		t.Fatalf("add new article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	var summarized []int64
	summarizeArticleFn = func(url string, forceExtractive bool, opts summarizer.Options) (summarizer.Result, error) {
		switch url {
		case oldArticle.URL:
			summarized = append(summarized, oldArticle.ID)
		case newArticle.URL:
			summarized = append(summarized, newArticle.ID)
		default:
			t.Fatalf("unexpected URL summarized: %s", url)
		}
		return summarizer.Result{Summary: "summary", Engine: summarizer.EngineSnippet}, nil
	}

	results, err := SummarizeArticlesDebugByFilterSince(db, false, "", AllInterestFilter(), &cutoff, false, false, 1, 1, summarizer.Options{}, nil, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize since: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != newArticle.ID {
		t.Fatalf("expected only new article, got %+v", results)
	}
	if len(summarized) != 1 || summarized[0] != newArticle.ID {
		t.Fatalf("expected only new article summarized, got %v", summarized)
	}
}

func TestClassifyArticlesInterestByFilterAppliesBeforeLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	hidden, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Hidden", URL: "https://example.com/hidden"})
	if err != nil {
		t.Fatalf("add hidden article: %v", err)
	}
	preferred, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Preferred", URL: "https://example.com/preferred"})
	if err != nil {
		t.Fatalf("add preferred article: %v", err)
	}
	for _, article := range []model.Article{hidden, preferred} {
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
	}
	if err := db.UpdateArticleInterest(hidden.ID, model.InterestStateHide, "test", "test", time.Now()); err != nil {
		t.Fatalf("update hidden interest: %v", err)
	}
	if err := db.UpdateArticleInterest(preferred.ID, model.InterestStatePrefer, "test", "test", time.Now()); err != nil {
		t.Fatalf("update preferred interest: %v", err)
	}

	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		classifyInterestFn = originalClassify
	})

	var classified int
	classifyInterestFn = func(blogName string, summary string, prompt string, opts interest.Options) (interest.Result, error) {
		classified++
		return interest.Result{State: model.InterestStateHide, Reason: "still hidden", Engine: interest.EngineOpenAI}, nil
	}

	filter, err := ParseInterestFilter([]string{"hide"})
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	results, err := ClassifyArticlesInterestByFilter(db, false, "", filter, true, false, false, 1, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "classify",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify filtered articles: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != hidden.ID {
		t.Fatalf("expected only hidden article, got %+v", results)
	}
	if classified != 1 {
		t.Fatalf("expected one classification call, got %d", classified)
	}
}

func TestClassifyArticlesInterestSinceAppliesBeforeLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	oldDate := time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)
	cutoff := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	newDate := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	oldArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Old", URL: "https://example.com/old", PublishedDate: &oldDate})
	if err != nil {
		t.Fatalf("add old article: %v", err)
	}
	newArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "New", URL: "https://example.com/new", PublishedDate: &newDate})
	if err != nil {
		t.Fatalf("add new article: %v", err)
	}
	for _, article := range []model.Article{oldArticle, newArticle} {
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
	}

	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		classifyInterestFn = originalClassify
	})

	var classified int
	classifyInterestFn = func(blogName string, summary string, prompt string, opts interest.Options) (interest.Result, error) {
		classified++
		return interest.Result{State: model.InterestStatePrefer, Reason: "test", Engine: interest.EngineOpenAI}, nil
	}

	results, err := ClassifyArticlesInterestDebugByFilterSince(db, false, "", AllInterestFilter(), &cutoff, false, false, false, 1, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "classify",
	}, nil, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify since: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != newArticle.ID {
		t.Fatalf("expected only new article, got %+v", results)
	}
	if classified != 1 {
		t.Fatalf("expected one classification call, got %d", classified)
	}
}

func TestGetArticlesByIDs(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	a1, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "First", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	_, err = db.AddArticle(model.Article{BlogID: blog.ID, Title: "Second", URL: "https://example.com/2"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	a3, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Third", URL: "https://example.com/3"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	articles, blogNames, err := GetArticlesByIDs(db, []int64{a1.ID, a3.ID})
	if err != nil {
		t.Fatalf("get by ids: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles, got %d", len(articles))
	}
	if articles[0].ID != a1.ID || articles[1].ID != a3.ID {
		t.Fatalf("unexpected article IDs: %d, %d", articles[0].ID, articles[1].ID)
	}
	if blogNames[blog.ID] != "Test" {
		t.Fatalf("expected blog name")
	}

	if _, _, err := GetArticlesByIDs(db, []int64{9999}); err == nil {
		t.Fatalf("expected article not found error")
	}
}

func TestExportBlogsScript(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if _, err := AddBlog(db, "Zeta's Blog", "https://zeta.example.com", "", "article h2 a[href*='post']"); err != nil {
		t.Fatalf("add blog: %v", err)
	}
	if _, err := AddBlog(db, "Alpha", "https://alpha.example.com", "https://alpha.example.com/feed.xml", ""); err != nil {
		t.Fatalf("add blog: %v", err)
	}
	if _, err := AddBlog(db, "-Daily Notes", "https://dash.example.com", "", "main a"); err != nil {
		t.Fatalf("add blog: %v", err)
	}

	script, err := ExportBlogsScript(db)
	if err != nil {
		t.Fatalf("export blogs: %v", err)
	}

	expected := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"",
		"# Recreate tracked blog definitions on another machine.",
		"# Usage: blogwatcher export > blogs.sh && sh blogs.sh",
		"",
		"blogwatcher add --scrape-selector 'main a' -- '-Daily Notes' 'https://dash.example.com'",
		"blogwatcher add --feed-url 'https://alpha.example.com/feed.xml' -- 'Alpha' 'https://alpha.example.com'",
		"blogwatcher add --scrape-selector 'article h2 a[href*='\"'\"'post'\"'\"']' -- 'Zeta'\"'\"'s Blog' 'https://zeta.example.com'",
		"",
	}, "\n")

	if script != expected {
		t.Fatalf("unexpected export script:\n%s", script)
	}
}

func TestExportBlogsScriptEmpty(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	script, err := ExportBlogsScript(db)
	if err != nil {
		t.Fatalf("export blogs: %v", err)
	}

	expected := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"",
		"# Recreate tracked blog definitions on another machine.",
		"# Usage: blogwatcher export > blogs.sh && sh blogs.sh",
		"# No blogs configured.",
		"",
	}, "\n")

	if script != expected {
		t.Fatalf("unexpected empty export script:\n%s", script)
	}
}

func TestSummarizeArticlesDoesNotCountCachedSummariesAgainstLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}

	for i := range 3 {
		article, err := db.AddArticle(model.Article{
			BlogID: blog.ID,
			Title:  "Title",
			URL:    fmt.Sprintf("https://example.com/%d", i+1),
		})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
	}

	results, err := SummarizeArticles(db, false, "", false, false, 2, 1, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, result := range results {
		if !result.Cached {
			t.Fatalf("expected cached result for article %d", result.Article.ID)
		}
	}
}

func TestSummarizeArticlesHackerNewsUsesCurrentScopeOnly(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}

	var unreadIDs []int64
	for i := range 3 {
		article, err := db.AddArticle(model.Article{
			BlogID: blog.ID,
			Title:  fmt.Sprintf("Title %d", i+1),
			URL:    fmt.Sprintf("https://example.com/%d", i+1),
		})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
		if i == 2 {
			if _, err := db.MarkArticleRead(article.ID); err != nil {
				t.Fatalf("mark read: %v", err)
			}
		} else {
			unreadIDs = append(unreadIDs, article.ID)
		}
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})

	var enrichedIDs []int64
	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		enrichedIDs = append(enrichedIDs, article.ID)
		return &hackernews.Result{
			ID:                1000 + article.ID,
			URL:               fmt.Sprintf("https://news.ycombinator.com/item?id=%d", 1000+article.ID),
			Points:            12,
			Comments:          3,
			DiscussionSummary: "HN discussion summary",
		}, nil
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   30,
	})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if len(results) != len(unreadIDs) {
		t.Fatalf("expected %d unread results, got %d", len(unreadIDs), len(results))
	}
	expectedIDs := map[int64]bool{}
	for _, id := range unreadIDs {
		expectedIDs[id] = true
	}
	for _, id := range enrichedIDs {
		if !expectedIDs[id] {
			t.Fatalf("unexpected HN enrichment for article %d; expected scope %v", id, unreadIDs)
		}
	}
	if len(enrichedIDs) != len(expectedIDs) {
		t.Fatalf("expected HN enrichment for %d articles, got %d", len(expectedIDs), len(enrichedIDs))
	}
	for _, result := range results {
		if result.HackerNews == nil {
			t.Fatalf("expected HN result for article %d", result.Article.ID)
		}
	}

	fetched, err := db.GetArticle(unreadIDs[0])
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched.HNSummary != "HN discussion summary" {
		t.Fatalf("expected HN summary cached, got %q", fetched.HNSummary)
	}
}

func TestSummarizeArticlesHackerNewsUsesCachedSummaryWithoutRefresh(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}
	if err := db.UpdateArticleHackerNews(article.ID, 42, 10, 3, "cached HN summary", time.Now().UTC()); err != nil {
		t.Fatalf("cache HN summary: %v", err)
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})
	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		if refresh {
			return &hackernews.Result{ID: 42, URL: "https://news.ycombinator.com/item?id=42", Points: 11, Comments: 4, DiscussionSummary: "fresh HN summary"}, nil
		}
		if article.HNSummary == "" {
			t.Fatalf("expected cached HN summary in article")
		}
		return &hackernews.Result{ID: article.HNItemID, URL: "https://news.ycombinator.com/item?id=42", Points: article.HNPoints, Comments: article.HNComments, DiscussionSummary: article.HNSummary, Cached: true}, nil
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   0,
	})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if results[0].HackerNews == nil || !results[0].HackerNews.Cached {
		t.Fatalf("expected cached HN result, got %+v", results[0].HackerNews)
	}

	results, err = SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Refresh: true,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("refresh HN: %v", err)
	}
	if results[0].HackerNews.DiscussionSummary != "fresh HN summary" {
		t.Fatalf("expected refreshed HN summary, got %q", results[0].HackerNews.DiscussionSummary)
	}
}

func TestSummarizeArticlesHackerNewsLimitCountsMissingSummaries(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	for i := range 2 {
		article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: fmt.Sprintf("Title %d", i), URL: fmt.Sprintf("https://example.com/%d", i)})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})
	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		t.Fatalf("HN enrichment should not start when --hn-limit is exceeded")
		return nil, nil
	}

	_, err = SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   1,
	})
	var limitErr HackerNewsLimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expected HN limit error, got %v", err)
	}
	if limitErr.Total != 2 || limitErr.Limit != 1 {
		t.Fatalf("unexpected HN limit error: %+v", limitErr)
	}
}

func TestSummarizeArticlesHackerNewsPersistsNotFoundMarkerAndRetries(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})

	var calls int
	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		calls++
		return &hackernews.Result{NotFound: true}, nil
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if results[0].HackerNews == nil || !results[0].HackerNews.NotFound {
		t.Fatalf("expected not found HN result, got %+v", results[0].HackerNews)
	}
	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched.HNItemID != 0 {
		t.Fatalf("expected HN item id 0 marker, got %d", fetched.HNItemID)
	}
	if fetched.HNFetched == nil {
		t.Fatalf("expected HN fetched timestamp for not found marker")
	}

	_, err = SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("second HN run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected not-found article to be retried, got %d calls", calls)
	}
}

func TestSummarizeArticlesHackerNewsZeroCommentsIsCachedComplete(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}
	if err := db.UpdateArticleHackerNews(article.ID, 42, 10, 0, "", time.Now().UTC()); err != nil {
		t.Fatalf("cache HN metadata: %v", err)
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   0,
	})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if results[0].HackerNews == nil || !results[0].HackerNews.Cached || results[0].HackerNews.Comments != 0 {
		t.Fatalf("expected cached zero-comment HN result, got %+v", results[0].HackerNews)
	}
}

func TestSummarizeArticlesHackerNewsSummaryFailureDoesNotPersistSummary(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})
	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		return &hackernews.Result{
			ID:                42,
			URL:               "https://news.ycombinator.com/item?id=42",
			Points:            10,
			Comments:          2,
			DiscussionSummary: "do not persist this failed summary",
			Warning:           "HN discussion summary failed: denied",
		}, nil
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{}, HackerNewsOptions{
		Enabled: true,
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if results[0].HackerNews.Warning == "" {
		t.Fatalf("expected transient HN warning")
	}
	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched.HNItemID != 42 || fetched.HNComments != 2 {
		t.Fatalf("expected HN metadata persisted, got item=%d comments=%d", fetched.HNItemID, fetched.HNComments)
	}
	if fetched.HNSummary != "" {
		t.Fatalf("expected failed HN summary not persisted, got %q", fetched.HNSummary)
	}
}

func TestSummarizeArticlesReturnsCacheWriteFailures(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalUpdate := updateSummaryFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		updateSummaryFn = originalUpdate
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{Summary: "fresh summary", Engine: summarizer.EngineSnippet}, nil
	}
	updateSummaryFn = func(*storage.Database, int64, string, string) error {
		return errors.New("write failed")
	}

	_, err = SummarizeArticles(db, false, "", false, false, 10, 2, summarizer.Options{}, HackerNewsOptions{})
	if err == nil {
		t.Fatalf("expected cache write error")
	}
	expected := fmt.Sprintf("failed to cache summary for article %d: write failed", article.ID)
	if err.Error() != expected {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSummarizeArticlesPropagatesFallbackWarningAndActualEngine(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	_, err = db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{
			Summary: "fallback summary",
			Engine:  summarizer.EngineSnippet,
			Warning: "OpenAI summarization failed: unauthorized. Fell back to snippet summarization.",
		}, nil
	}

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{OpenAIAPIKey: "configured"}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Engine != summarizer.EngineSnippet {
		t.Fatalf("expected actual engine %q, got %q", summarizer.EngineSnippet, results[0].Engine)
	}
	if results[0].Warning == "" {
		t.Fatalf("expected fallback warning")
	}
}

func TestClassifyArticleInterestAutoGeneratesSummaryAndCachesResult(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	originalUpdateInterest := updateInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
		updateInterestFn = originalUpdateInterest
	})

	summarizeCalls := 0
	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		summarizeCalls++
		return summarizer.Result{Summary: "cached summary", Engine: summarizer.EngineSnippet}, nil
	}

	classifyCalls := 0
	classifyInterestFn = func(blogName string, summary string, prompt string, opts interest.Options) (interest.Result, error) {
		classifyCalls++
		if blogName != "Tech Blog" {
			t.Fatalf("expected blog name, got %q", blogName)
		}
		if summary != "cached summary" {
			t.Fatalf("expected summary to be reused, got %q", summary)
		}
		if prompt != "Prefer compiler posts." {
			t.Fatalf("expected prompt rule, got %q", prompt)
		}
		return interest.Result{State: model.InterestStatePrefer, Reason: "Compiler internals", Engine: interest.EngineOpenAI}, nil
	}

	result, err := ClassifyArticleInterest(db, article.ID, false, false, false, summarizer.Options{}, config.InterestConfig{
		Model:          "gpt-5.4-nano",
		InterestPrompt: "Default prompt",
		Blogs: map[string]config.InterestBlogConfig{
			"Tech Blog": {InterestPrompt: "Prefer compiler posts."},
		},
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify article interest: %v", err)
	}
	if summarizeCalls != 1 {
		t.Fatalf("expected 1 summary call, got %d", summarizeCalls)
	}
	if classifyCalls != 1 {
		t.Fatalf("expected 1 classify call, got %d", classifyCalls)
	}
	if result.Article.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected prefer state, got %q", result.Article.InterestState)
	}

	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched == nil {
		t.Fatalf("expected fetched article")
	}
	if fetched.Summary != "cached summary" {
		t.Fatalf("expected cached summary, got %q", fetched.Summary)
	}
	if fetched.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected cached interest state, got %q", fetched.InterestState)
	}
	if fetched.InterestReason != "Compiler internals" {
		t.Fatalf("expected cached interest reason, got %q", fetched.InterestReason)
	}
}

func TestClassifyArticlesInterestDoesNotCountCachedResultsAgainstLimit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}

	for i := range 3 {
		article, err := db.AddArticle(model.Article{
			BlogID: blog.ID,
			Title:  "Title",
			URL:    fmt.Sprintf("https://example.com/%d", i+1),
		})
		if err != nil {
			t.Fatalf("add article: %v", err)
		}
		if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
			t.Fatalf("cache summary: %v", err)
		}
		if err := db.UpdateArticleInterest(article.ID, model.InterestStateNormal, "Looks fine", interest.EngineOpenAI, time.Now().UTC()); err != nil {
			t.Fatalf("cache interest: %v", err)
		}
	}

	results, err := ClassifyArticlesInterest(db, false, "", false, false, false, 2, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer technical posts.",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify articles interest: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, result := range results {
		if !result.Cached {
			t.Fatalf("expected cached result for article %d", result.Article.ID)
		}
	}
}

func TestClassifyArticlesInterestHackerNewsEnrichesResults(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "cached summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}
	if err := db.UpdateArticleInterest(article.ID, model.InterestStatePrefer, "reason", interest.EngineOpenAI, time.Now()); err != nil {
		t.Fatalf("cache interest: %v", err)
	}

	originalEnrich := enrichHackerNewsFn
	t.Cleanup(func() {
		enrichHackerNewsFn = originalEnrich
	})

	enrichHackerNewsFn = func(article model.Article, opts summarizer.Options, refresh bool) (*hackernews.Result, error) {
		return &hackernews.Result{
			ID:                42,
			URL:               "https://news.ycombinator.com/item?id=42",
			Points:            5,
			Comments:          2,
			DiscussionSummary: "HN discussion summary",
		}, nil
	}

	results, err := ClassifyArticlesInterest(db, false, "", false, false, false, 10, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer technical posts.",
	}, HackerNewsOptions{Enabled: true, Limit: 30})
	if err != nil {
		t.Fatalf("classify articles interest: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].HackerNews == nil {
		t.Fatalf("expected HN enrichment")
	}
}

func TestClassifyArticlesInterestReturnsCacheWriteFailures(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}

	originalClassify := classifyInterestFn
	originalUpdate := updateInterestFn
	t.Cleanup(func() {
		classifyInterestFn = originalClassify
		updateInterestFn = originalUpdate
	})

	classifyInterestFn = func(string, string, string, interest.Options) (interest.Result, error) {
		return interest.Result{State: model.InterestStateHide, Reason: "Low signal", Engine: interest.EngineOpenAI}, nil
	}
	updateInterestFn = func(*storage.Database, int64, string, string, string, time.Time) error {
		return errors.New("write failed")
	}

	_, err = ClassifyArticlesInterest(db, false, "", false, false, false, 10, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Hide low-signal posts.",
	}, HackerNewsOptions{})
	if err == nil {
		t.Fatalf("expected cache write error")
	}
	if !strings.Contains(err.Error(), "failed to cache interest for article") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClassifyArticleInterestRefreshSummaryBypassesCachedInterest(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "old summary", summarizer.EngineSnippet); err != nil {
		t.Fatalf("cache summary: %v", err)
	}
	if err := db.UpdateArticleInterest(article.ID, model.InterestStateNormal, "Old reason", interest.EngineOpenAI, time.Now().UTC()); err != nil {
		t.Fatalf("cache interest: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{Summary: "new summary", Engine: summarizer.EngineSnippet}, nil
	}

	classifyCalls := 0
	classifyInterestFn = func(string, string, string, interest.Options) (interest.Result, error) {
		classifyCalls++
		return interest.Result{State: model.InterestStatePrefer, Reason: "Fresh reason", Engine: interest.EngineOpenAI}, nil
	}

	result, err := ClassifyArticleInterest(db, article.ID, false, true, false, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer fresh technical writeups.",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify article interest: %v", err)
	}
	if classifyCalls != 1 {
		t.Fatalf("expected fresh classification, got %d calls", classifyCalls)
	}
	if result.Article.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected refreshed interest state, got %q", result.Article.InterestState)
	}
}

func TestClassifyArticleInterestSkipsWhenPromptMissing(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		t.Fatalf("did not expect summary generation when prompt is missing")
		return summarizer.Result{}, nil
	}
	classifyInterestFn = func(string, string, string, interest.Options) (interest.Result, error) {
		t.Fatalf("did not expect classification when prompt is missing")
		return interest.Result{}, nil
	}

	result, err := ClassifyArticleInterest(db, article.ID, false, false, false, summarizer.Options{}, config.InterestConfig{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify article interest: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("expected classification to be skipped")
	}
	if result.Note == "" {
		t.Fatalf("expected skip note")
	}

	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched == nil {
		t.Fatalf("expected fetched article")
	}
	if fetched.InterestState != "" || fetched.Summary != "" {
		t.Fatalf("expected article to remain unclassified and unsummarized: %+v", fetched)
	}
}

func TestClassifyArticleInterestSkipsWhenClassificationFails(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{Summary: "summary", Engine: summarizer.EngineSnippet}, nil
	}
	classifyInterestFn = func(string, string, string, interest.Options) (interest.Result, error) {
		return interest.Result{}, errors.New("classifier unavailable")
	}

	result, err := ClassifyArticleInterest(db, article.ID, false, false, false, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer technical posts.",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify article interest: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("expected classification to be skipped")
	}
	if !strings.Contains(result.Note, "classifier unavailable") {
		t.Fatalf("expected classifier error in note, got %q", result.Note)
	}

	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched == nil {
		t.Fatalf("expected fetched article")
	}
	if fetched.InterestState != "" {
		t.Fatalf("expected article to remain unclassified: %+v", fetched)
	}
	if fetched.Summary != "summary" {
		t.Fatalf("expected summary to stay cached, got %+v", fetched)
	}
}

func TestClassifyArticlesInterestSkipsSummaryFailuresAndContinues(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	failingArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Failing", URL: "https://example.com/fail"})
	if err != nil {
		t.Fatalf("add failing article: %v", err)
	}
	okArticle, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "OK", URL: "https://example.com/ok"})
	if err != nil {
		t.Fatalf("add ok article: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
	})

	summarizeArticleFn = func(url string, _ bool, _ summarizer.Options) (summarizer.Result, error) {
		if strings.Contains(url, "/fail") {
			return summarizer.Result{}, errors.New("failed to fetch article https://example.com/fail: status 403")
		}
		return summarizer.Result{Summary: "working summary", Engine: summarizer.EngineSnippet}, nil
	}
	classifyInterestFn = func(blogName string, summary string, prompt string, opts interest.Options) (interest.Result, error) {
		if summary != "working summary" {
			t.Fatalf("unexpected summary %q", summary)
		}
		return interest.Result{State: model.InterestStatePrefer, Reason: "Useful", Engine: interest.EngineOpenAI}, nil
	}

	results, err := ClassifyArticlesInterest(db, false, "", false, false, false, 10, 1, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer technical posts.",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify articles interest: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Article.ID != failingArticle.ID {
		t.Fatalf("expected first result for failing article, got %d", results[0].Article.ID)
	}
	if !results[0].Skipped {
		t.Fatalf("expected first result to be skipped")
	}
	if !strings.Contains(results[0].Note, "status 403") {
		t.Fatalf("expected fetch error note, got %q", results[0].Note)
	}

	if results[1].Article.ID != okArticle.ID {
		t.Fatalf("expected second result for ok article, got %d", results[1].Article.ID)
	}
	if results[1].Skipped {
		t.Fatalf("expected second result to be classified")
	}
	if results[1].Article.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected prefer state, got %q", results[1].Article.InterestState)
	}

	failedFetched, err := db.GetArticle(failingArticle.ID)
	if err != nil {
		t.Fatalf("get failing article: %v", err)
	}
	if failedFetched == nil {
		t.Fatalf("expected failing article")
	}
	if failedFetched.InterestState != "" {
		t.Fatalf("expected failing article to remain unclassified: %+v", failedFetched)
	}

	okFetched, err := db.GetArticle(okArticle.ID)
	if err != nil {
		t.Fatalf("get ok article: %v", err)
	}
	if okFetched == nil {
		t.Fatalf("expected ok article")
	}
	if okFetched.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected ok article to be classified, got %+v", okFetched)
	}
}

func TestSummarizeArticlePreservesRSSSummaryOnFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	rssSummary := longRSSSummary(600)

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, rssSummary, summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{}, fmt.Errorf("failed to fetch article: status 403")
	}

	result, err := SummarizeArticle(db, article.ID, false, true, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if result.Article.Summary != rssSummary {
		t.Fatalf("expected RSS summary preserved")
	}
	if result.Article.SummaryEngine != summarizer.EngineRSS {
		t.Fatalf("expected rss engine preserved, got %q", result.Article.SummaryEngine)
	}
	if result.Warning == "" {
		t.Fatalf("expected warning about failed summarization")
	}
	if !result.Cached {
		t.Fatalf("expected cached=true when falling back to RSS summary")
	}
}

func TestSummarizeArticlesPreservesRSSSummaryOnFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	rssSummary := longRSSSummary(600)

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, rssSummary, summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{}, fmt.Errorf("failed to fetch article: status 403")
	}

	results, err := SummarizeArticles(db, false, "", false, true, 10, 1, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize articles: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Article.Summary != rssSummary {
		t.Fatalf("expected RSS summary preserved")
	}
	if results[0].Warning == "" {
		t.Fatalf("expected warning about failed summarization")
	}

	fetched, err := db.GetArticle(article.ID)
	if err != nil {
		t.Fatalf("get article: %v", err)
	}
	if fetched.Summary != rssSummary {
		t.Fatalf("expected RSS summary in DB preserved")
	}
	if fetched.SummaryEngine != summarizer.EngineRSS {
		t.Fatalf("expected rss engine in DB preserved, got %q", fetched.SummaryEngine)
	}
}

func TestSummarizeArticleLongRSSTreatedAsCached(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	rssSummary := longRSSSummary(600)

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, rssSummary, summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		t.Fatalf("should not be called — long RSS summary should be treated as cached")
		return summarizer.Result{}, nil
	}

	result, err := SummarizeArticle(db, article.ID, false, false, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if !result.Cached {
		t.Fatalf("expected cached=true for long RSS summary without --refresh")
	}
	if result.Article.Summary != rssSummary {
		t.Fatalf("expected RSS summary preserved")
	}
}

func TestSummarizeArticleShortRSSAutoUpgraded(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "Short RSS blurb.", summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	upgraded := false
	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		upgraded = true
		return summarizer.Result{Summary: "Full LLM summary of the article.", Engine: summarizer.EngineOpenAI}, nil
	}

	result, err := SummarizeArticle(db, article.ID, false, false, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if !upgraded {
		t.Fatalf("expected short RSS summary to trigger re-summarization")
	}
	if result.Cached {
		t.Fatalf("expected cached=false for upgraded summary")
	}
	if !result.Upgraded {
		t.Fatalf("expected upgraded=true for short RSS auto-upgrade")
	}
	if result.Article.Summary != "Full LLM summary of the article." {
		t.Fatalf("expected upgraded summary, got %q", result.Article.Summary)
	}
	if result.Article.SummaryEngine != summarizer.EngineOpenAI {
		t.Fatalf("expected openai engine, got %q", result.Article.SummaryEngine)
	}
}

func TestSummarizeArticleShortRSSPreservedOnFailure(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Test", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "Short RSS blurb.", summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
	})

	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		return summarizer.Result{}, fmt.Errorf("failed to fetch article: status 403")
	}

	result, err := SummarizeArticle(db, article.ID, false, false, summarizer.Options{}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("summarize article: %v", err)
	}
	if result.Article.Summary != "Short RSS blurb." {
		t.Fatalf("expected short RSS summary preserved on failure, got %q", result.Article.Summary)
	}
	if result.Warning == "" {
		t.Fatalf("expected warning about failed summarization")
	}
}

func TestClassifyInterestAutoUpgradesShortRSSSummary(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	blog, err := AddBlog(db, "Tech Blog", "https://example.com", "", "")
	if err != nil {
		t.Fatalf("add blog: %v", err)
	}
	article, err := db.AddArticle(model.Article{BlogID: blog.ID, Title: "Title", URL: "https://example.com/1"})
	if err != nil {
		t.Fatalf("add article: %v", err)
	}
	if err := db.UpdateArticleSummary(article.ID, "Short RSS blurb.", summarizer.EngineRSS); err != nil {
		t.Fatalf("cache rss summary: %v", err)
	}

	originalSummarize := summarizeArticleFn
	originalClassify := classifyInterestFn
	t.Cleanup(func() {
		summarizeArticleFn = originalSummarize
		classifyInterestFn = originalClassify
	})

	summarizeCalls := 0
	summarizeArticleFn = func(string, bool, summarizer.Options) (summarizer.Result, error) {
		summarizeCalls++
		return summarizer.Result{Summary: "Upgraded full summary.", Engine: summarizer.EngineOpenAI}, nil
	}
	classifyInterestFn = func(blogName string, summary string, prompt string, opts interest.Options) (interest.Result, error) {
		if summary != "Upgraded full summary." {
			t.Fatalf("expected upgraded summary for classification, got %q", summary)
		}
		return interest.Result{State: model.InterestStatePrefer, Reason: "Good stuff", Engine: interest.EngineOpenAI}, nil
	}

	result, err := ClassifyArticleInterest(db, article.ID, false, false, false, summarizer.Options{}, config.InterestConfig{
		InterestPrompt: "Prefer technical posts.",
	}, HackerNewsOptions{})
	if err != nil {
		t.Fatalf("classify article interest: %v", err)
	}
	if summarizeCalls != 1 {
		t.Fatalf("expected short RSS to trigger summarization, got %d calls", summarizeCalls)
	}
	if result.Article.InterestState != model.InterestStatePrefer {
		t.Fatalf("expected prefer state, got %q", result.Article.InterestState)
	}
}

func TestSummaryDebugTag(t *testing.T) {
	tests := []struct {
		name     string
		result   SummaryResult
		expected string
	}{
		{"cached", SummaryResult{Cached: true}, " (cached)"},
		{"upgraded", SummaryResult{Upgraded: true}, " (upgraded-rss)"},
		{"fresh", SummaryResult{}, ""},
		{"cached takes precedence over upgraded", SummaryResult{Cached: true, Upgraded: true}, " (cached)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := summaryDebugTag(tc.result); got != tc.expected {
				t.Fatalf("summaryDebugTag() = %q, want %q", got, tc.expected)
			}
		})
	}
}

// longRSSSummary returns a string of exactly n characters for testing RSS summary length thresholds.
func longRSSSummary(n int) string {
	base := "This is a detailed RSS feed description with enough content to be useful for interest classification. "
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(base)
	}
	return b.String()[:n]
}

func openTestDB(t *testing.T) *storage.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blogwatcher.db")
	db, err := storage.OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

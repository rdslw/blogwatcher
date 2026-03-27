package controller

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
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

	articles, blogNames, err := GetArticles(db, false, "")
	if err != nil {
		t.Fatalf("get articles: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected article")
	}
	if blogNames[blog.ID] != blog.Name {
		t.Fatalf("expected blog name")
	}

	if _, _, err := GetArticles(db, false, "Missing"); err == nil {
		t.Fatalf("expected blog not found error")
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

	results, err := SummarizeArticles(db, false, "", false, false, 2, 1, summarizer.Options{})
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

	_, err = SummarizeArticles(db, false, "", false, false, 10, 2, summarizer.Options{})
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

	results, err := SummarizeArticles(db, false, "", false, false, 10, 1, summarizer.Options{OpenAIAPIKey: "configured"})
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
		Model:         "gpt-5.4-nano",
		DefaultPrompt: "Default prompt",
		Blogs: map[string]config.InterestBlogConfig{
			"Tech Blog": {InterestPrompt: "Prefer compiler posts."},
		},
	})
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
		DefaultPrompt: "Prefer technical posts.",
	})
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
		DefaultPrompt: "Hide low-signal posts.",
	})
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
		DefaultPrompt: "Prefer fresh technical writeups.",
	})
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

	result, err := ClassifyArticleInterest(db, article.ID, false, false, false, summarizer.Options{}, config.InterestConfig{})
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

func openTestDB(t *testing.T) *storage.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blogwatcher.db")
	db, err := storage.OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

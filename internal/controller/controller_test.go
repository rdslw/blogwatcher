package controller

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Hyaxia/blogwatcher/internal/model"
	"github.com/Hyaxia/blogwatcher/internal/storage"
	"github.com/Hyaxia/blogwatcher/internal/summarizer"
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

func openTestDB(t *testing.T) *storage.Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "blogwatcher.db")
	db, err := storage.OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

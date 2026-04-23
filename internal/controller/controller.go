package controller

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/debug"
	"github.com/rdslw/blogwatcher/internal/interest"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/storage"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

// debugTrivialThreshold is the duration below which an article operation
// (summary or interest classification) is considered trivially fast (cached
// or skipped). Individual debug lines are suppressed for these; a single
// summary line is emitted instead.
const debugTrivialThreshold = 10 * time.Millisecond

// rssSummaryMinChars is the minimum length for an RSS-sourced summary to be
// considered sufficient. Shorter RSS descriptions (typical 1-2 sentence blurbs
// from feeds like OpenAI or DeepMind) are treated as empty and auto-upgraded
// to a full summary on the next summary or interest run — no --refresh needed.
const rssSummaryMinChars = 500

var (
	summarizeArticleFn = summarizer.SummarizeArticle
	classifyInterestFn = interest.ClassifySummary
	openDatabaseFn     = storage.OpenDatabase
	updateSummaryFn    = func(db *storage.Database, id int64, summary string, engine string) error {
		return db.UpdateArticleSummary(id, summary, engine)
	}
	updateInterestFn = func(db *storage.Database, id int64, state string, reason string, engine string, judgedAt time.Time) error {
		return db.UpdateArticleInterest(id, state, reason, engine, judgedAt)
	}
)

// isRSSSummaryShort returns true when the article has an RSS-sourced summary
// that is too short for reliable interest classification. Such summaries are
// treated as empty by the summary and interest pipelines so they get
// auto-upgraded to a full summary without requiring --refresh.
func isRSSSummaryShort(article model.Article) bool {
	return article.SummaryEngine == summarizer.EngineRSS &&
		len([]rune(article.Summary)) < rssSummaryMinChars
}

type LimitExceededError struct {
	Limit int
	Total int
}

func (e LimitExceededError) Error() string {
	return fmt.Sprintf("ALERT: %d articles found but limit is %d. Use --limit to increase or narrow results with --blog.", e.Total, e.Limit)
}

type BlogNotFoundError struct {
	Name string
}

func (e BlogNotFoundError) Error() string {
	return fmt.Sprintf("Blog '%s' not found", e.Name)
}

type BlogAlreadyExistsError struct {
	Field string
	Value string
}

func (e BlogAlreadyExistsError) Error() string {
	return fmt.Sprintf("Blog with %s '%s' already exists", e.Field, e.Value)
}

type ArticleNotFoundError struct {
	ID int64
}

func (e ArticleNotFoundError) Error() string {
	return fmt.Sprintf("Article %d not found", e.ID)
}

func AddBlog(db *storage.Database, name string, url string, feedURL string, scrapeSelector string) (model.Blog, error) {
	if existing, err := db.GetBlogByName(name); err != nil {
		return model.Blog{}, err
	} else if existing != nil {
		return model.Blog{}, BlogAlreadyExistsError{Field: "name", Value: name}
	}
	if existing, err := db.GetBlogByURL(url); err != nil {
		return model.Blog{}, err
	} else if existing != nil {
		return model.Blog{}, BlogAlreadyExistsError{Field: "URL", Value: url}
	}

	blog := model.Blog{
		Name:           name,
		URL:            url,
		FeedURL:        feedURL,
		ScrapeSelector: scrapeSelector,
	}
	return db.AddBlog(blog)
}

func RemoveBlog(db *storage.Database, name string) error {
	blog, err := db.GetBlogByName(name)
	if err != nil {
		return err
	}
	if blog == nil {
		return BlogNotFoundError{Name: name}
	}
	_, err = db.RemoveBlog(blog.ID)
	return err
}

func GetArticles(db *storage.Database, showAll bool, blogName string, interestFilter string) ([]model.Article, map[int64]string, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(blogName)
		if err != nil {
			return nil, nil, err
		}
		if blog == nil {
			return nil, nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	articles, err := db.ListArticles(!showAll, blogID)
	if err != nil {
		return nil, nil, err
	}

	articles = filterByInterest(articles, interestFilter)

	blogNames, err := buildBlogNames(db)
	if err != nil {
		return nil, nil, err
	}

	return articles, blogNames, nil
}

func GetArticlesByIDs(db *storage.Database, ids []int64) ([]model.Article, map[int64]string, error) {
	var articles []model.Article
	for _, id := range ids {
		article, err := db.GetArticle(id)
		if err != nil {
			return nil, nil, err
		}
		if article == nil {
			return nil, nil, ArticleNotFoundError{ID: id}
		}
		articles = append(articles, *article)
	}

	blogNames, err := buildBlogNames(db)
	if err != nil {
		return nil, nil, err
	}

	return articles, blogNames, nil
}

func buildBlogNames(db *storage.Database) (map[int64]string, error) {
	blogs, err := db.ListBlogs()
	if err != nil {
		return nil, err
	}
	blogNames := make(map[int64]string)
	for _, blog := range blogs {
		blogNames[blog.ID] = blog.Name
	}
	return blogNames, nil
}

func filterByInterest(articles []model.Article, filter string) []model.Article {
	switch filter {
	case "prefer":
		filtered := articles[:0:0]
		for _, a := range articles {
			if a.InterestState == model.InterestStatePrefer {
				filtered = append(filtered, a)
			}
		}
		return filtered
	case "norm":
		filtered := articles[:0:0]
		for _, a := range articles {
			if a.InterestState != model.InterestStateHide {
				filtered = append(filtered, a)
			}
		}
		return filtered
	default:
		return articles
	}
}

func ExportBlogsScript(db *storage.Database) (string, error) {
	blogs, err := db.ListBlogs()
	if err != nil {
		return "", err
	}

	var out strings.Builder
	out.WriteString("#!/bin/sh\n")
	out.WriteString("set -eu\n\n")
	out.WriteString("# Recreate tracked blog definitions on another machine.\n")
	out.WriteString("# Usage: blogwatcher export > blogs.sh && sh blogs.sh\n")

	if len(blogs) == 0 {
		out.WriteString("# No blogs configured.\n")
		return out.String(), nil
	}

	out.WriteString("\n")
	for _, blog := range blogs {
		out.WriteString("blogwatcher add")
		if blog.FeedURL != "" {
			out.WriteString(" --feed-url ")
			out.WriteString(shellQuote(blog.FeedURL))
		}
		if blog.ScrapeSelector != "" {
			out.WriteString(" --scrape-selector ")
			out.WriteString(shellQuote(blog.ScrapeSelector))
		}
		out.WriteString(" -- ")
		out.WriteString(shellQuote(blog.Name))
		out.WriteString(" ")
		out.WriteString(shellQuote(blog.URL))
		out.WriteString("\n")
	}

	return out.String(), nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func MarkArticleRead(db *storage.Database, articleID int64) (model.Article, error) {
	article, err := db.GetArticle(articleID)
	if err != nil {
		return model.Article{}, err
	}
	if article == nil {
		return model.Article{}, ArticleNotFoundError{ID: articleID}
	}
	if !article.IsRead {
		_, err = db.MarkArticleRead(articleID)
		if err != nil {
			return model.Article{}, err
		}
	}
	return *article, nil
}

func MarkArticlesReadByScope(db *storage.Database, blogName string, scope string) ([]model.Article, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(blogName)
		if err != nil {
			return nil, err
		}
		if blog == nil {
			return nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	articles, err := db.ListArticles(true, blogID)
	if err != nil {
		return nil, err
	}

	if scope != "all" {
		filtered := articles[:0:0]
		for _, a := range articles {
			if a.InterestState == scope {
				filtered = append(filtered, a)
			}
		}
		articles = filtered
	}

	for _, article := range articles {
		_, err := db.MarkArticleRead(article.ID)
		if err != nil {
			return nil, err
		}
	}

	return articles, nil
}

type SummaryResult struct {
	Article  model.Article
	BlogName string
	Engine   string
	Cached   bool
	Upgraded bool
	Warning  string
}

type InterestResult struct {
	Article  model.Article
	BlogName string
	Engine   string
	Cached   bool
	Skipped  bool
	Note     string
}

func SummarizeArticle(db *storage.Database, articleID int64, forceExtractive bool, refresh bool, opts summarizer.Options) (SummaryResult, error) {
	article, err := db.GetArticle(articleID)
	if err != nil {
		return SummaryResult{}, err
	}
	if article == nil {
		return SummaryResult{}, ArticleNotFoundError{ID: articleID}
	}

	blog, err := db.GetBlog(article.BlogID)
	if err != nil {
		return SummaryResult{}, err
	}
	blogName := ""
	if blog != nil {
		blogName = blog.Name
	}

	upgraded := isRSSSummaryShort(*article)
	if article.Summary != "" && !refresh && !upgraded {
		engine := article.SummaryEngine
		if engine == "" {
			engine = "unknown"
		}
		return SummaryResult{Article: *article, BlogName: blogName, Engine: engine, Cached: true}, nil
	}

	result, err := summarizeArticleFn(article.URL, forceExtractive, opts)
	if err != nil {
		if article.Summary != "" && article.SummaryEngine == summarizer.EngineRSS {
			return SummaryResult{
				Article:  *article,
				BlogName: blogName,
				Engine:   article.SummaryEngine,
				Cached:   true,
				Warning:  fmt.Sprintf("Summarization failed: %v. Kept existing RSS summary.", err),
			}, nil
		}
		return SummaryResult{}, fmt.Errorf("failed to summarize article %d: %v", articleID, err)
	}

	if err := updateSummaryFn(db, article.ID, result.Summary, result.Engine); err != nil {
		return SummaryResult{}, err
	}
	article.Summary = result.Summary
	article.SummaryEngine = result.Engine

	return SummaryResult{Article: *article, BlogName: blogName, Engine: result.Engine, Cached: false, Upgraded: upgraded, Warning: result.Warning}, nil
}

func SummarizeArticles(db *storage.Database, showAll bool, blogName string, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options) ([]SummaryResult, error) {
	return SummarizeArticlesDebug(db, showAll, blogName, forceExtractive, refresh, limit, workers, opts, nil)
}

func SummarizeArticlesDebug(db *storage.Database, showAll bool, blogName string, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options, dbg *debug.Logger) ([]SummaryResult, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(blogName)
		if err != nil {
			return nil, err
		}
		if blog == nil {
			return nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	articles, err := db.ListArticles(!showAll, blogID)
	if err != nil {
		return nil, err
	}

	if limit > 0 {
		articlesToSummarize := 0
		for _, article := range articles {
			if refresh || article.Summary == "" || isRSSSummaryShort(article) {
				articlesToSummarize++
			}
		}
		if articlesToSummarize > limit {
			return nil, LimitExceededError{Limit: limit, Total: articlesToSummarize}
		}
	}

	blogs, err := db.ListBlogs()
	if err != nil {
		return nil, err
	}
	blogNames := make(map[int64]string)
	for _, b := range blogs {
		blogNames[b.ID] = b.Name
	}

	phaseStart := time.Now()
	dbg.Log("summary phase: %d article(s), workers=%d", len(articles), workers)
	results := make([]SummaryResult, len(articles))

	if workers <= 1 {
		var skipped int
		var processed int
		for i, article := range articles {
			t := time.Now()
			result, err := summarizeOne(db, article, blogNames[article.BlogID], forceExtractive, refresh, opts)
			if err != nil {
				return nil, err
			}
			elapsed := time.Since(t)
			if elapsed < debugTrivialThreshold {
				skipped++
			} else {
				processed++
				dbg.Log("summarize article=%d %q engine=%s%s (%s)", article.ID, article.Title, result.Engine, summaryDebugTag(result), elapsed)
			}
			results[i] = result
		}
		if skipped > 0 {
			dbg.Log("summarize skipped %d cached article(s)", skipped)
		}
		dbg.Log("summary phase done: %d processed, %d cached, total %s", processed, skipped, time.Since(phaseStart))
		return results, nil
	}

	type job struct {
		Index    int
		Article  model.Article
		BlogName string
	}
	jobs := make(chan job, len(articles))

	for i, article := range articles {
		jobs <- job{Index: i, Article: article, BlogName: blogNames[article.BlogID]}
	}
	close(jobs)

	var (
		wg       sync.WaitGroup
		firstErr error
		errMu    sync.Mutex
	)

	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	var skippedCount atomic.Int64
	var processedCount atomic.Int64

	for i := 0; i < workers; i++ {
		workerID := i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			tag := fmt.Sprintf("[worker-%d] ", workerID)

			workerDB, err := openDatabaseFn(db.Path())
			if err != nil {
				setErr(err)
				return
			}
			defer workerDB.Close()

			for item := range jobs {
				t := time.Now()
				result, err := summarizeOne(workerDB, item.Article, item.BlogName, forceExtractive, refresh, opts)
				if err != nil {
					setErr(err)
					continue
				}
				elapsed := time.Since(t)
				if elapsed < debugTrivialThreshold {
					skippedCount.Add(1)
				} else {
					processedCount.Add(1)
					dbg.Log("%ssummarize article=%d %q engine=%s%s (%s)", tag, item.Article.ID, item.Article.Title, result.Engine, summaryDebugTag(result), elapsed)
				}
				results[item.Index] = result
			}
		}()
	}

	wg.Wait()
	if s := skippedCount.Load(); s > 0 {
		dbg.Log("summarize skipped %d cached article(s)", s)
	}
	dbg.Log("summary phase done: %d processed, %d cached, total %s", processedCount.Load(), skippedCount.Load(), time.Since(phaseStart))
	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

func ClassifyArticleInterest(db *storage.Database, articleID int64, refresh bool, summaryRefresh bool, forceExtractive bool, summaryOpts summarizer.Options, interestCfg config.InterestConfig) (InterestResult, error) {
	article, err := db.GetArticle(articleID)
	if err != nil {
		return InterestResult{}, err
	}
	if article == nil {
		return InterestResult{}, ArticleNotFoundError{ID: articleID}
	}

	blog, err := db.GetBlog(article.BlogID)
	if err != nil {
		return InterestResult{}, err
	}
	blogName := ""
	if blog != nil {
		blogName = blog.Name
	}

	return classifyOne(db, *article, blogName, refresh, summaryRefresh, forceExtractive, summaryOpts, interestCfg)
}

func ClassifyArticlesInterest(db *storage.Database, showAll bool, blogName string, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig) ([]InterestResult, error) {
	return ClassifyArticlesInterestDebug(db, showAll, blogName, refresh, summaryRefresh, forceExtractive, limit, workers, summaryOpts, interestCfg, nil)
}

func ClassifyArticlesInterestDebug(db *storage.Database, showAll bool, blogName string, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig, dbg *debug.Logger) ([]InterestResult, error) {
	var blogID *int64
	if blogName != "" {
		blog, err := db.GetBlogByName(blogName)
		if err != nil {
			return nil, err
		}
		if blog == nil {
			return nil, BlogNotFoundError{Name: blogName}
		}
		blogID = &blog.ID
	}

	articles, err := db.ListArticles(!showAll, blogID)
	if err != nil {
		return nil, err
	}

	blogs, err := db.ListBlogs()
	if err != nil {
		return nil, err
	}
	blogNames := make(map[int64]string)
	for _, b := range blogs {
		blogNames[b.ID] = b.Name
	}

	if limit > 0 {
		articlesToClassify := 0
		for _, article := range articles {
			prompt := strings.TrimSpace(interestCfg.PromptForBlog(blogNames[article.BlogID]))
			if prompt == "" {
				continue
			}
			if refresh || summaryRefresh || article.InterestState == "" {
				articlesToClassify++
			}
		}
		if articlesToClassify > limit {
			return nil, LimitExceededError{Limit: limit, Total: articlesToClassify}
		}
	}

	phaseStart := time.Now()
	dbg.Log("interest phase: %d article(s), workers=%d", len(articles), workers)
	results := make([]InterestResult, len(articles))

	if workers <= 1 {
		var skipped int
		var processed int
		for i, article := range articles {
			t := time.Now()
			result, err := classifyOne(db, article, blogNames[article.BlogID], refresh, summaryRefresh, forceExtractive, summaryOpts, interestCfg)
			if err != nil {
				return nil, err
			}
			elapsed := time.Since(t)
			if elapsed < debugTrivialThreshold {
				skipped++
			} else {
				processed++
				label := result.Article.InterestState
				if result.Skipped {
					label = "skipped"
				} else if result.Cached {
					label += " (cached)"
				}
				dbg.Log("classify article=%d %q state=%s (%s)", article.ID, article.Title, label, elapsed)
			}
			results[i] = result
		}
		if skipped > 0 {
			dbg.Log("classify skipped %d article(s) (cached/no prompt)", skipped)
		}
		dbg.Log("interest phase done: %d processed, %d cached/skipped, total %s", processed, skipped, time.Since(phaseStart))
		return results, nil
	}

	type job struct {
		Index    int
		Article  model.Article
		BlogName string
	}
	jobs := make(chan job, len(articles))

	for i, article := range articles {
		jobs <- job{Index: i, Article: article, BlogName: blogNames[article.BlogID]}
	}
	close(jobs)

	var (
		wg       sync.WaitGroup
		firstErr error
		errMu    sync.Mutex
	)

	setErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	var skippedCount atomic.Int64
	var processedCount atomic.Int64

	for i := 0; i < workers; i++ {
		workerID := i + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			tag := fmt.Sprintf("[worker-%d] ", workerID)

			workerDB, err := openDatabaseFn(db.Path())
			if err != nil {
				setErr(err)
				return
			}
			defer workerDB.Close()

			for item := range jobs {
				t := time.Now()
				result, err := classifyOne(workerDB, item.Article, item.BlogName, refresh, summaryRefresh, forceExtractive, summaryOpts, interestCfg)
				if err != nil {
					setErr(err)
					continue
				}
				elapsed := time.Since(t)
				if elapsed < debugTrivialThreshold {
					skippedCount.Add(1)
				} else {
					processedCount.Add(1)
					label := result.Article.InterestState
					if result.Skipped {
						label = "skipped"
					} else if result.Cached {
						label += " (cached)"
					}
					dbg.Log("%sclassify article=%d %q state=%s (%s)", tag, item.Article.ID, item.Article.Title, label, elapsed)
				}
				results[item.Index] = result
			}
		}()
	}

	wg.Wait()
	if s := skippedCount.Load(); s > 0 {
		dbg.Log("classify skipped %d article(s) (cached/no prompt)", s)
	}
	dbg.Log("interest phase done: %d processed, %d cached/skipped, total %s", processedCount.Load(), skippedCount.Load(), time.Since(phaseStart))
	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

func summarizeOne(db *storage.Database, article model.Article, blogName string, forceExtractive bool, refresh bool, opts summarizer.Options) (SummaryResult, error) {
	cached := false
	upgraded := isRSSSummaryShort(article)
	engine := article.SummaryEngine
	if engine == "" {
		engine = "unknown"
	}
	if article.Summary != "" && !refresh && !upgraded {
		cached = true
	} else {
		result, err := summarizeArticleFn(article.URL, forceExtractive, opts)
		if err != nil {
			if article.Summary != "" && article.SummaryEngine == summarizer.EngineRSS {
				return SummaryResult{
					Article:  article,
					BlogName: blogName,
					Engine:   engine,
					Cached:   true,
					Warning:  fmt.Sprintf("Summarization failed: %v. Kept existing RSS summary.", err),
				}, nil
			}
			return SummaryResult{
				Article:  article,
				BlogName: blogName,
				Engine:   engine,
			}, nil
		}

		if err := updateSummaryFn(db, article.ID, result.Summary, result.Engine); err != nil {
			return SummaryResult{}, fmt.Errorf("failed to cache summary for article %d: %w", article.ID, err)
		}
		article.Summary = result.Summary
		article.SummaryEngine = result.Engine
		engine = result.Engine
		return SummaryResult{
			Article:  article,
			BlogName: blogName,
			Engine:   engine,
			Cached:   cached,
			Upgraded: upgraded,
			Warning:  result.Warning,
		}, nil
	}
	return SummaryResult{
		Article:  article,
		BlogName: blogName,
		Engine:   engine,
		Cached:   cached,
	}, nil
}

func summaryDebugTag(r SummaryResult) string {
	if r.Cached {
		return " (cached)"
	}
	if r.Upgraded {
		return " (upgraded-rss)"
	}
	return ""
}

func classifyOne(db *storage.Database, article model.Article, blogName string, refresh bool, summaryRefresh bool, forceExtractive bool, summaryOpts summarizer.Options, interestCfg config.InterestConfig) (InterestResult, error) {
	engine := article.InterestEngine
	if engine == "" {
		engine = "unknown"
	}
	prompt := strings.TrimSpace(interestCfg.PromptForBlog(blogName))
	if prompt == "" {
		return InterestResult{
			Article:  article,
			BlogName: blogName,
			Skipped:  true,
			Note:     "No interest prompt configured; left unclassified.",
		}, nil
	}
	if article.InterestState != "" && !refresh && !summaryRefresh {
		return InterestResult{
			Article:  article,
			BlogName: blogName,
			Engine:   engine,
			Cached:   true,
		}, nil
	}

	articleWithSummary, err := ensureArticleSummary(db, article, forceExtractive, summaryRefresh, summaryOpts)
	if err != nil {
		return skippedInterestResult(article, blogName, engine, fmt.Sprintf("Failed to generate summary before interest classification: %v. Left unclassified.", err)), nil
	}

	result, err := classifyInterestFn(blogName, articleWithSummary.Summary, prompt, interest.OptionsFromConfig(interestCfg))
	if err != nil {
		return skippedInterestResult(articleWithSummary, blogName, engine, fmt.Sprintf("Failed to classify interest: %v. Left unclassified.", err)), nil
	}

	judgedAt := time.Now().UTC()
	if err := updateInterestFn(db, article.ID, result.State, result.Reason, result.Engine, judgedAt); err != nil {
		return InterestResult{}, fmt.Errorf("failed to cache interest for article %d: %w", article.ID, err)
	}

	articleWithSummary.InterestState = result.State
	articleWithSummary.InterestReason = result.Reason
	articleWithSummary.InterestEngine = result.Engine
	articleWithSummary.InterestJudged = &judgedAt

	return InterestResult{
		Article:  articleWithSummary,
		BlogName: blogName,
		Engine:   result.Engine,
		Cached:   false,
	}, nil
}

func skippedInterestResult(article model.Article, blogName string, engine string, note string) InterestResult {
	return InterestResult{
		Article:  article,
		BlogName: blogName,
		Engine:   engine,
		Skipped:  true,
		Note:     note,
	}
}

func ensureArticleSummary(db *storage.Database, article model.Article, forceExtractive bool, refresh bool, opts summarizer.Options) (model.Article, error) {
	if article.Summary != "" && !refresh && !isRSSSummaryShort(article) {
		return article, nil
	}

	result, err := summarizeArticleFn(article.URL, forceExtractive, opts)
	if err != nil {
		if article.Summary != "" && article.SummaryEngine == summarizer.EngineRSS {
			return article, nil
		}
		return model.Article{}, fmt.Errorf("failed to summarize article %d before interest classification: %w", article.ID, err)
	}
	if err := updateSummaryFn(db, article.ID, result.Summary, result.Engine); err != nil {
		return model.Article{}, fmt.Errorf("failed to cache summary for article %d: %w", article.ID, err)
	}

	article.Summary = result.Summary
	article.SummaryEngine = result.Engine
	return article, nil
}

func MarkArticleUnread(db *storage.Database, articleID int64) (model.Article, error) {
	article, err := db.GetArticle(articleID)
	if err != nil {
		return model.Article{}, err
	}
	if article == nil {
		return model.Article{}, ArticleNotFoundError{ID: articleID}
	}
	if article.IsRead {
		_, err = db.MarkArticleUnread(articleID)
		if err != nil {
			return model.Article{}, err
		}
	}
	return *article, nil
}

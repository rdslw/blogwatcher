package controller

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/debug"
	"github.com/rdslw/blogwatcher/internal/hackernews"
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
	enrichHackerNewsFn = hackernews.EnrichArticle
	openDatabaseFn     = storage.OpenDatabase
	updateSummaryFn    = func(db *storage.Database, id int64, summary string, engine string) error {
		return db.UpdateArticleSummary(id, summary, engine)
	}
	updateInterestFn = func(db *storage.Database, id int64, state string, reason string, engine string, judgedAt time.Time) error {
		return db.UpdateArticleInterest(id, state, reason, engine, judgedAt)
	}
	updateHackerNewsFn = func(db *storage.Database, id int64, itemID int64, points int, comments int, summary string, fetched time.Time) error {
		return db.UpdateArticleHackerNews(id, itemID, points, comments, summary, fetched)
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

type InvalidInterestFilterError struct {
	Value string
}

func (e InvalidInterestFilterError) Error() string {
	return fmt.Sprintf("invalid --filter value %q: must be all, hide, normal/norm, or prefer/pref", e.Value)
}

type InterestFilter struct {
	all                 bool
	includeHide         bool
	includeNormal       bool
	includePrefer       bool
	includeUnclassified bool
}

func ParseInterestFilter(values []string) (InterestFilter, error) {
	if len(values) == 0 {
		return AllInterestFilter(), nil
	}

	var filter InterestFilter
	var sawAll bool
	var sawSpecific bool
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			token := strings.ToLower(strings.TrimSpace(part))
			if token == "" {
				continue
			}
			switch token {
			case "all":
				sawAll = true
				filter.all = true
			case "hide":
				sawSpecific = true
				filter.includeHide = true
			case "normal", "norm":
				sawSpecific = true
				filter.includeNormal = true
				filter.includeUnclassified = true
			case "prefer", "pref":
				sawSpecific = true
				filter.includePrefer = true
			default:
				return InterestFilter{}, InvalidInterestFilterError{Value: token}
			}
		}
	}

	if sawAll && sawSpecific {
		return InterestFilter{}, fmt.Errorf("cannot combine --filter all with other filter values")
	}
	if filter.all || (!filter.includeHide && !filter.includeNormal && !filter.includePrefer && !filter.includeUnclassified) {
		return AllInterestFilter(), nil
	}
	return filter, nil
}

func AllInterestFilter() InterestFilter {
	return InterestFilter{all: true}
}

func (f InterestFilter) Match(article model.Article) bool {
	if f.all {
		return true
	}
	switch article.InterestState {
	case model.InterestStateHide:
		return f.includeHide
	case model.InterestStateNormal:
		return f.includeNormal
	case model.InterestStatePrefer:
		return f.includePrefer
	case "":
		return f.includeUnclassified
	default:
		return false
	}
}

func (f InterestFilter) String() string {
	if f.all {
		return "all"
	}
	var parts []string
	if f.includeHide {
		parts = append(parts, "hide")
	}
	if f.includeNormal {
		parts = append(parts, "normal")
	}
	if f.includePrefer {
		parts = append(parts, "prefer")
	}
	return strings.Join(parts, ",")
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
	filter, err := ParseInterestFilter([]string{interestFilter})
	if err != nil {
		return nil, nil, err
	}
	return GetArticlesByFilter(db, showAll, blogName, filter)
}

func GetArticlesByFilter(db *storage.Database, showAll bool, blogName string, filter InterestFilter) ([]model.Article, map[int64]string, error) {
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

	articles = filterByInterest(articles, filter)

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

func filterByInterest(articles []model.Article, filter InterestFilter) []model.Article {
	if filter.all {
		return articles
	}
	filtered := articles[:0:0]
	for _, a := range articles {
		if filter.Match(a) {
			filtered = append(filtered, a)
		}
	}
	return filtered
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

func MarkArticlesReadByFilter(db *storage.Database, blogName string, filter InterestFilter) ([]model.Article, error) {
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

	articles = filterByInterest(articles, filter)

	for _, article := range articles {
		_, err := db.MarkArticleRead(article.ID)
		if err != nil {
			return nil, err
		}
	}

	return articles, nil
}

type SummaryResult struct {
	Article    model.Article
	BlogName   string
	Engine     string
	Cached     bool
	Upgraded   bool
	Warning    string
	HackerNews *hackernews.Result
}

type InterestResult struct {
	Article    model.Article
	BlogName   string
	Engine     string
	Cached     bool
	Skipped    bool
	Note       string
	HackerNews *hackernews.Result
}

type HackerNewsOptions struct {
	Enabled bool
	Refresh bool
	Limit   int
	Options summarizer.Options
}

type HackerNewsLimitExceededError struct {
	Limit int
	Total int
}

func (e HackerNewsLimitExceededError) Error() string {
	return fmt.Sprintf("ALERT: %d selected article(s) need Hacker News lookup/summary but --hn-limit is %d. Increase --hn-limit to continue.", e.Total, e.Limit)
}

func SummarizeArticle(db *storage.Database, articleID int64, forceExtractive bool, refresh bool, opts summarizer.Options, hnOpts HackerNewsOptions) (SummaryResult, error) {
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
		result := SummaryResult{Article: *article, BlogName: blogName, Engine: engine, Cached: true}
		return enrichSummaryResult(db, result, hnOpts)
	}

	result, err := summarizeArticleFn(article.URL, forceExtractive, opts)
	if err != nil {
		if article.Summary != "" && article.SummaryEngine == summarizer.EngineRSS {
			result := SummaryResult{
				Article:  *article,
				BlogName: blogName,
				Engine:   article.SummaryEngine,
				Cached:   true,
				Warning:  fmt.Sprintf("Summarization failed: %v. Kept existing RSS summary.", err),
			}
			return enrichSummaryResult(db, result, hnOpts)
		}
		return SummaryResult{}, fmt.Errorf("failed to summarize article %d: %v", articleID, err)
	}

	if err := updateSummaryFn(db, article.ID, result.Summary, result.Engine); err != nil {
		return SummaryResult{}, err
	}
	article.Summary = result.Summary
	article.SummaryEngine = result.Engine

	summaryResult := SummaryResult{Article: *article, BlogName: blogName, Engine: result.Engine, Cached: false, Upgraded: upgraded, Warning: result.Warning}
	return enrichSummaryResult(db, summaryResult, hnOpts)
}

func SummarizeArticles(db *storage.Database, showAll bool, blogName string, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options, hnOpts HackerNewsOptions) ([]SummaryResult, error) {
	return SummarizeArticlesDebug(db, showAll, blogName, forceExtractive, refresh, limit, workers, opts, nil, hnOpts)
}

func SummarizeArticlesByFilter(db *storage.Database, showAll bool, blogName string, filter InterestFilter, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options, hnOpts HackerNewsOptions) ([]SummaryResult, error) {
	return SummarizeArticlesDebugByFilter(db, showAll, blogName, filter, forceExtractive, refresh, limit, workers, opts, nil, hnOpts)
}

func SummarizeArticlesDebug(db *storage.Database, showAll bool, blogName string, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options, dbg *debug.Logger, hnOpts HackerNewsOptions) ([]SummaryResult, error) {
	return SummarizeArticlesDebugByFilter(db, showAll, blogName, AllInterestFilter(), forceExtractive, refresh, limit, workers, opts, dbg, hnOpts)
}

func SummarizeArticlesDebugByFilter(db *storage.Database, showAll bool, blogName string, filter InterestFilter, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options, dbg *debug.Logger, hnOpts HackerNewsOptions) ([]SummaryResult, error) {
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
	articles = filterByInterest(articles, filter)

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
		return enrichSummaryResults(db, results, hnOpts)
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

	return enrichSummaryResults(db, results, hnOpts)
}

func ClassifyArticleInterest(db *storage.Database, articleID int64, refresh bool, summaryRefresh bool, forceExtractive bool, summaryOpts summarizer.Options, interestCfg config.InterestConfig, hnOpts HackerNewsOptions) (InterestResult, error) {
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

	result, err := classifyOne(db, *article, blogName, refresh, summaryRefresh, forceExtractive, summaryOpts, interestCfg)
	if err != nil {
		return result, err
	}
	return enrichInterestResult(db, result, hnOpts)
}

func ClassifyArticlesInterest(db *storage.Database, showAll bool, blogName string, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig, hnOpts HackerNewsOptions) ([]InterestResult, error) {
	return ClassifyArticlesInterestDebug(db, showAll, blogName, refresh, summaryRefresh, forceExtractive, limit, workers, summaryOpts, interestCfg, nil, hnOpts)
}

func ClassifyArticlesInterestByFilter(db *storage.Database, showAll bool, blogName string, filter InterestFilter, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig, hnOpts HackerNewsOptions) ([]InterestResult, error) {
	return ClassifyArticlesInterestDebugByFilter(db, showAll, blogName, filter, refresh, summaryRefresh, forceExtractive, limit, workers, summaryOpts, interestCfg, nil, hnOpts)
}

func ClassifyArticlesInterestDebug(db *storage.Database, showAll bool, blogName string, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig, dbg *debug.Logger, hnOpts HackerNewsOptions) ([]InterestResult, error) {
	return ClassifyArticlesInterestDebugByFilter(db, showAll, blogName, AllInterestFilter(), refresh, summaryRefresh, forceExtractive, limit, workers, summaryOpts, interestCfg, dbg, hnOpts)
}

func ClassifyArticlesInterestDebugByFilter(db *storage.Database, showAll bool, blogName string, filter InterestFilter, refresh bool, summaryRefresh bool, forceExtractive bool, limit int, workers int, summaryOpts summarizer.Options, interestCfg config.InterestConfig, dbg *debug.Logger, hnOpts HackerNewsOptions) ([]InterestResult, error) {
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
	articles = filterByInterest(articles, filter)

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
		return enrichInterestResults(db, results, hnOpts)
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

	return enrichInterestResults(db, results, hnOpts)
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

// enrichArticleHN fetches HN data for one article and persists it when fresh.
// It returns the updated article, the HN result, an optional transient note
// (e.g. when the HN lookup itself failed), and a non-nil error only for
// hard failures the caller must propagate (e.g. cache write failure).
func enrichArticleHN(db *storage.Database, article model.Article, opts HackerNewsOptions) (model.Article, *hackernews.Result, string, error) {
	hn, err := enrichHackerNewsFn(article, opts.Options, opts.Refresh)
	if err != nil {
		return article, nil, fmt.Sprintf("HN lookup failed: %v", err), nil
	}
	if hn != nil && !hn.Cached {
		fetched := time.Now().UTC()
		summary := hn.DiscussionSummary
		if hn.Warning != "" {
			summary = ""
		}
		if err := updateHackerNewsFn(db, article.ID, hn.ID, hn.Points, hn.Comments, summary, fetched); err != nil {
			return article, nil, "", fmt.Errorf("failed to cache HN data for article %d: %w", article.ID, err)
		}
		article.HNItemID = hn.ID
		article.HNPoints = hn.Points
		article.HNComments = hn.Comments
		article.HNSummary = summary
		article.HNFetched = &fetched
	}
	return article, hn, "", nil
}

func enrichSummaryResults(db *storage.Database, results []SummaryResult, opts HackerNewsOptions) ([]SummaryResult, error) {
	if opts.Enabled {
		work := 0
		for _, r := range results {
			if needsHackerNewsWork(r.Article, opts.Refresh) {
				work++
			}
		}
		if err := checkHackerNewsLimit(opts, work); err != nil {
			return nil, err
		}
		for i := range results {
			article, hn, note, err := enrichArticleHN(db, results[i].Article, opts)
			if err != nil {
				return nil, err
			}
			results[i].Article = article
			results[i].HackerNews = hn
			if note != "" {
				results[i].Warning = appendNote(results[i].Warning, note)
			}
		}
	}
	for i := range results {
		if results[i].HackerNews == nil {
			results[i].HackerNews = cachedHackerNewsResult(results[i].Article)
		}
	}
	return results, nil
}

func enrichSummaryResult(db *storage.Database, result SummaryResult, opts HackerNewsOptions) (SummaryResult, error) {
	if opts.Enabled {
		work := 0
		if needsHackerNewsWork(result.Article, opts.Refresh) {
			work = 1
		}
		if err := checkHackerNewsLimit(opts, work); err != nil {
			return result, err
		}
		article, hn, note, err := enrichArticleHN(db, result.Article, opts)
		if err != nil {
			return SummaryResult{}, err
		}
		result.Article = article
		result.HackerNews = hn
		if note != "" {
			result.Warning = appendNote(result.Warning, note)
		}
	}
	if result.HackerNews == nil {
		result.HackerNews = cachedHackerNewsResult(result.Article)
	}
	return result, nil
}

func enrichInterestResults(db *storage.Database, results []InterestResult, opts HackerNewsOptions) ([]InterestResult, error) {
	if opts.Enabled {
		work := 0
		for _, r := range results {
			if needsHackerNewsWork(r.Article, opts.Refresh) {
				work++
			}
		}
		if err := checkHackerNewsLimit(opts, work); err != nil {
			return nil, err
		}
		for i := range results {
			article, hn, note, err := enrichArticleHN(db, results[i].Article, opts)
			if err != nil {
				return nil, err
			}
			results[i].Article = article
			results[i].HackerNews = hn
			if note != "" {
				results[i].Note = appendNote(results[i].Note, note)
			}
		}
	}
	for i := range results {
		if results[i].HackerNews == nil {
			results[i].HackerNews = cachedHackerNewsResult(results[i].Article)
		}
	}
	return results, nil
}

func enrichInterestResult(db *storage.Database, result InterestResult, opts HackerNewsOptions) (InterestResult, error) {
	if opts.Enabled {
		work := 0
		if needsHackerNewsWork(result.Article, opts.Refresh) {
			work = 1
		}
		if err := checkHackerNewsLimit(opts, work); err != nil {
			return result, err
		}
		article, hn, note, err := enrichArticleHN(db, result.Article, opts)
		if err != nil {
			return InterestResult{}, err
		}
		result.Article = article
		result.HackerNews = hn
		if note != "" {
			result.Note = appendNote(result.Note, note)
		}
	}
	if result.HackerNews == nil {
		result.HackerNews = cachedHackerNewsResult(result.Article)
	}
	return result, nil
}

// cachedHackerNewsResult builds a *hackernews.Result from cached DB fields so
// callers can surface previously fetched HN data even when --hn is off.
// Returns nil when the article has never been checked.
func cachedHackerNewsResult(article model.Article) *hackernews.Result {
	if article.HNFetched == nil {
		return nil
	}
	if article.HNItemID == 0 {
		return &hackernews.Result{NotFound: true, Cached: true}
	}
	return &hackernews.Result{
		ID:                article.HNItemID,
		URL:               hackernews.ItemURL(article.HNItemID),
		Points:            article.HNPoints,
		Comments:          article.HNComments,
		DiscussionSummary: article.HNSummary,
		Cached:            true,
	}
}

func checkHackerNewsLimit(opts HackerNewsOptions, total int) error {
	if !opts.Enabled || opts.Limit < 0 || total <= opts.Limit {
		return nil
	}
	return HackerNewsLimitExceededError{Limit: opts.Limit, Total: total}
}

func needsHackerNewsWork(article model.Article, refresh bool) bool {
	if refresh {
		return true
	}
	if article.HNItemID == 0 {
		return true
	}
	if article.HNComments == 0 && article.HNFetched != nil {
		return false
	}
	return strings.TrimSpace(article.HNSummary) == ""
}

func appendNote(existing string, note string) string {
	if strings.TrimSpace(existing) == "" {
		return note
	}
	return existing + " " + note
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

package controller

import (
	"fmt"
	"sync"

	"github.com/Hyaxia/blogwatcher/internal/model"
	"github.com/Hyaxia/blogwatcher/internal/storage"
	"github.com/Hyaxia/blogwatcher/internal/summarizer"
)

var (
	summarizeArticleFn = summarizer.SummarizeArticle
	openDatabaseFn     = storage.OpenDatabase
	updateSummaryFn    = func(db *storage.Database, id int64, summary string, engine string) error {
		return db.UpdateArticleSummary(id, summary, engine)
	}
)

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

func GetArticles(db *storage.Database, showAll bool, blogName string) ([]model.Article, map[int64]string, error) {
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
	blogs, err := db.ListBlogs()
	if err != nil {
		return nil, nil, err
	}
	blogNames := make(map[int64]string)
	for _, blog := range blogs {
		blogNames[blog.ID] = blog.Name
	}

	return articles, blogNames, nil
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

func MarkAllArticlesRead(db *storage.Database, blogName string) ([]model.Article, error) {
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
	Warning  string
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

	if article.Summary != "" && !refresh {
		engine := article.SummaryEngine
		if engine == "" {
			engine = "unknown"
		}
		return SummaryResult{Article: *article, BlogName: blogName, Engine: engine, Cached: true}, nil
	}

	result, err := summarizer.SummarizeArticle(article.URL, forceExtractive, opts)
	if err != nil {
		return SummaryResult{}, fmt.Errorf("failed to summarize article %d: %v", articleID, err)
	}

	if err := updateSummaryFn(db, article.ID, result.Summary, result.Engine); err != nil {
		return SummaryResult{}, err
	}
	article.Summary = result.Summary
	article.SummaryEngine = result.Engine

	return SummaryResult{Article: *article, BlogName: blogName, Engine: result.Engine, Cached: false, Warning: result.Warning}, nil
}

func SummarizeArticles(db *storage.Database, showAll bool, blogName string, forceExtractive bool, refresh bool, limit int, workers int, opts summarizer.Options) ([]SummaryResult, error) {
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
			if refresh || article.Summary == "" {
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

	results := make([]SummaryResult, len(articles))

	if workers <= 1 {
		for i, article := range articles {
			result, err := summarizeOne(db, article, blogNames[article.BlogID], forceExtractive, refresh, opts)
			if err != nil {
				return nil, err
			}
			results[i] = result
		}
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

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			workerDB, err := openDatabaseFn(db.Path())
			if err != nil {
				setErr(err)
				return
			}
			defer workerDB.Close()

			for item := range jobs {
				result, err := summarizeOne(workerDB, item.Article, item.BlogName, forceExtractive, refresh, opts)
				if err != nil {
					setErr(err)
					continue
				}
				results[item.Index] = result
			}
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	return results, nil
}

func summarizeOne(db *storage.Database, article model.Article, blogName string, forceExtractive bool, refresh bool, opts summarizer.Options) (SummaryResult, error) {
	cached := false
	engine := article.SummaryEngine
	if engine == "" {
		engine = "unknown"
	}
	if article.Summary != "" && !refresh {
		cached = true
	} else {
		result, err := summarizeArticleFn(article.URL, forceExtractive, opts)
		if err != nil {
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

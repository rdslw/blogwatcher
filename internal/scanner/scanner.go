package scanner

import (
	"fmt"
	"time"

	"github.com/rdslw/blogwatcher/internal/debug"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/rss"
	"github.com/rdslw/blogwatcher/internal/scraper"
	"github.com/rdslw/blogwatcher/internal/storage"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

type ScanResult struct {
	BlogName    string
	NewArticles int
	TotalFound  int
	Source      string
	Error       string
}

func ScanBlog(db *storage.Database, blog model.Blog) ScanResult {
	return ScanBlogDebug(db, blog, false, "", nil)
}

func ScanBlogDebug(db *storage.Database, blog model.Blog, feedDiscovery bool, workerTag string, dbg *debug.Logger) ScanResult {
	blogStart := time.Now()
	dbg.Log("%sscan start blog=%q url=%s", workerTag, blog.Name, blog.URL)

	var (
		articles []model.Article
		source   = "none"
		errText  string
	)

	feedURL := blog.FeedURL
	if feedURL == "" {
		// Skip feed discovery for blogs that already have a scrape selector,
		// unless --feed-discovery is explicitly requested. Probing common feed
		// paths on sites without RSS can waste tens of seconds per blog.
		if blog.ScrapeSelector != "" && !feedDiscovery {
			dbg.Log("%s  skipping feed discovery (scrape selector set)", workerTag)
		} else {
			dbg.Log("%s  discovering feed for %q", workerTag, blog.Name)
			t := time.Now()
			if discovered, err := rss.DiscoverFeedURL(blog.URL, 15*time.Second); err == nil && discovered != "" {
				feedURL = discovered
				blog.FeedURL = discovered
				_ = db.UpdateBlog(blog)
				dbg.Log("%s  discovered feed=%s (%s)", workerTag, discovered, time.Since(t))
			} else {
				dbg.Log("%s  no feed discovered (%s)", workerTag, time.Since(t))
			}
		}
	}

	if feedURL != "" {
		dbg.Log("%s  parsing RSS feed for %q", workerTag, blog.Name)
		t := time.Now()
		feedArticles, err := rss.ParseFeed(feedURL, 30*time.Second)
		if err != nil {
			errText = err.Error()
			dbg.Log("%s  RSS parse failed: %v (%s)", workerTag, err, time.Since(t))
		} else {
			articles = convertFeedArticles(blog.ID, feedArticles)
			source = "rss"
			dbg.Log("%s  RSS parsed %d articles (%s)", workerTag, len(articles), time.Since(t))
		}
	}

	if len(articles) == 0 && blog.ScrapeSelector != "" {
		dbg.Log("%s  scraping HTML for %q selector=%q", workerTag, blog.Name, blog.ScrapeSelector)
		t := time.Now()
		scrapedArticles, err := scraper.ScrapeBlog(blog.URL, blog.ScrapeSelector, 30*time.Second)
		if err != nil {
			if errText != "" {
				errText = fmt.Sprintf("RSS: %s; Scraper: %s", errText, err.Error())
			} else {
				errText = err.Error()
			}
			dbg.Log("%s  scrape failed: %v (%s)", workerTag, err, time.Since(t))
		} else {
			articles = convertScrapedArticles(blog.ID, scrapedArticles)
			source = "scraper"
			errText = ""
			dbg.Log("%s  scraped %d articles (%s)", workerTag, len(articles), time.Since(t))
		}
	}

	seenURLs := make(map[string]struct{})
	uniqueArticles := make([]model.Article, 0, len(articles))
	for _, article := range articles {
		if _, exists := seenURLs[article.URL]; exists {
			continue
		}
		seenURLs[article.URL] = struct{}{}
		uniqueArticles = append(uniqueArticles, article)
	}

	urlList := make([]string, 0, len(seenURLs))
	for url := range seenURLs {
		urlList = append(urlList, url)
	}

	existing, err := db.GetExistingArticleURLs(urlList)
	if err != nil {
		errText = err.Error()
	}

	discoveredAt := time.Now()
	newArticles := make([]model.Article, 0, len(uniqueArticles))
	for _, article := range uniqueArticles {
		if _, exists := existing[article.URL]; exists {
			continue
		}
		article.DiscoveredDate = &discoveredAt
		newArticles = append(newArticles, article)
	}

	newCount := 0
	if len(newArticles) > 0 {
		count, err := db.AddArticlesBulk(newArticles)
		if err != nil {
			errText = err.Error()
		} else {
			newCount = count
		}
	}

	_ = db.UpdateBlogLastScanned(blog.ID, time.Now())

	dbg.Log("%sscan done  blog=%q source=%s found=%d new=%d (%s)", workerTag, blog.Name, source, len(seenURLs), newCount, time.Since(blogStart))

	return ScanResult{
		BlogName:    blog.Name,
		NewArticles: newCount,
		TotalFound:  len(seenURLs),
		Source:      source,
		Error:       errText,
	}
}

func ScanAllBlogs(db *storage.Database, workers int) ([]ScanResult, error) {
	return ScanAllBlogsDebug(db, workers, false, nil)
}

func ScanAllBlogsDebug(db *storage.Database, workers int, feedDiscovery bool, dbg *debug.Logger) ([]ScanResult, error) {
	blogs, err := db.ListBlogs()
	if err != nil {
		return nil, err
	}
	dbg.Log("scan phase: %d blog(s), workers=%d", len(blogs), workers)
	if workers <= 1 {
		results := make([]ScanResult, 0, len(blogs))
		for _, blog := range blogs {
			results = append(results, ScanBlogDebug(db, blog, feedDiscovery, "", dbg))
		}
		return results, nil
	}

	type job struct {
		Index int
		Blog  model.Blog
	}
	jobs := make(chan job)
	results := make([]ScanResult, len(blogs))
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		workerID := i + 1
		go func() {
			tag := fmt.Sprintf("[worker-%d] ", workerID)
			workerDB, err := storage.OpenDatabase(db.Path())
			if err != nil {
				errs <- err
				return
			}
			defer workerDB.Close()
			for item := range jobs {
				results[item.Index] = ScanBlogDebug(workerDB, item.Blog, feedDiscovery, tag, dbg)
			}
			errs <- nil
		}()
	}

	for index, blog := range blogs {
		jobs <- job{Index: index, Blog: blog}
	}
	close(jobs)

	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			return nil, err
		}
	}

	return results, nil
}

func ScanBlogByName(db *storage.Database, name string) (*ScanResult, error) {
	return ScanBlogByNameDebug(db, name, false, nil)
}

func ScanBlogByNameDebug(db *storage.Database, name string, feedDiscovery bool, dbg *debug.Logger) (*ScanResult, error) {
	blog, err := db.GetBlogByName(name)
	if err != nil {
		return nil, err
	}
	if blog == nil {
		return nil, nil
	}
	result := ScanBlogDebug(db, *blog, feedDiscovery, "", dbg)
	return &result, nil
}

const rssSummaryMaxChars = 2000

func convertFeedArticles(blogID int64, articles []rss.FeedArticle) []model.Article {
	result := make([]model.Article, 0, len(articles))
	for _, article := range articles {
		a := model.Article{
			BlogID:        blogID,
			Title:         article.Title,
			URL:           article.URL,
			PublishedDate: article.PublishedDate,
			IsRead:        false,
		}
		if desc := summarizer.StripHTMLTags(article.Description); desc != "" {
			a.Summary = summarizer.TruncateText(desc, rssSummaryMaxChars)
			a.SummaryEngine = summarizer.EngineRSS
		}
		result = append(result, a)
	}
	return result
}

func convertScrapedArticles(blogID int64, articles []scraper.ScrapedArticle) []model.Article {
	result := make([]model.Article, 0, len(articles))
	for _, article := range articles {
		result = append(result, model.Article{
			BlogID:        blogID,
			Title:         article.Title,
			URL:           article.URL,
			PublishedDate: article.PublishedDate,
			IsRead:        false,
		})
	}
	return result
}

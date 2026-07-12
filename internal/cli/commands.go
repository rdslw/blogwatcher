package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/rdslw/blogwatcher/internal/config"
	"github.com/rdslw/blogwatcher/internal/controller"
	"github.com/rdslw/blogwatcher/internal/debug"
	"github.com/rdslw/blogwatcher/internal/hackernews"
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/scanner"
	"github.com/rdslw/blogwatcher/internal/skill"
	"github.com/rdslw/blogwatcher/internal/storage"
	"github.com/rdslw/blogwatcher/internal/summarizer"
)

func newAddCommand() *cobra.Command {
	var feedURL string
	var scrapeSelector string

	cmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a new blog to track.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			url := args[1]
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()
			_, err = controller.AddBlog(db, name, url, feedURL, scrapeSelector)
			if err != nil {
				printError(err)
				return markError(err)
			}
			color.New(color.FgGreen).Printf("Added blog '%s'\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&feedURL, "feed-url", "", "RSS/Atom feed URL (auto-discovered if not provided)")
	cmd.Flags().StringVar(&scrapeSelector, "scrape-selector", "", "CSS selector for HTML scraping fallback")
	return cmd
}

func newRemoveCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a blog from tracking.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !yes {
				confirmed, err := confirm(fmt.Sprintf("Remove blog '%s' and all its articles?", name))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()
			if err := controller.RemoveBlog(db, name); err != nil {
				printError(err)
				return markError(err)
			}
			color.New(color.FgGreen).Printf("Removed blog '%s'\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old-name> <new-name>",
		Short: "Rename a tracked blog.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldName := args[0]
			newName := args[1]
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if err := controller.RenameBlog(db, oldName, newName); err != nil {
				printError(err)
				return markError(err)
			}
			color.New(color.FgGreen).Printf("Renamed blog '%s' to '%s'\n", oldName, newName)
			return nil
		},
	}
}

func newBlogsCommand() *cobra.Command {
	var verbose bool
	var jsonOut bool
	var unreadOnly bool

	cmd := &cobra.Command{
		Use:   "blogs",
		Short: "List all tracked blogs.",
		Long: `List all tracked blogs.

Entries interest labels apply to unread articles: "a/b/c h/n/p" means
hide/normal/prefer counts; "none h/n/p", "no interest data", and
"partial interest data" describe zero, missing, or partial unread interest data.

Use --unread to show only blogs with unread articles. Unlike --filter on
article commands, --unread filters blogs by unread count, not interest state.

Use --json for a machine-readable list (id, url, feed_url, scrape_selector,
last_scanned, stats) for agentic use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			defer db.Close()
			blogs, err := db.ListBlogs()
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			overviews, err := loadBlogOverviews(db, blogs, unreadOnly)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			if jsonOut {
				out := make([]jsonBlog, 0, len(overviews))
				for _, overview := range overviews {
					out = append(out, toJSONBlog(overview.blog, overview.stats))
				}
				return emitJSON(struct {
					Blogs []jsonBlog `json:"blogs"`
				}{Blogs: out})
			}

			if len(blogs) == 0 {
				fmt.Println("No blogs tracked yet. Use 'blogwatcher add' to add one.")
				return nil
			}
			if len(overviews) == 0 {
				fmt.Println("No blogs have unread articles.")
				return nil
			}
			heading := "Tracked blogs"
			if unreadOnly {
				heading = "Tracked blogs with unread articles"
			}
			color.New(color.FgCyan, color.Bold).Printf("%s (%d):\n\n", heading, len(overviews))
			for _, overview := range overviews {
				blog := overview.blog
				stats := overview.stats
				color.New(color.FgWhite, color.Bold).Printf("  %s\n", blog.Name)
				fmt.Printf("    URL: %s\n", blog.URL)
				if verbose && blog.FeedURL != "" {
					fmt.Printf("    Feed: %s\n", blog.FeedURL)
				}
				if verbose && blog.ScrapeSelector != "" {
					fmt.Printf("    Selector: %s\n", blog.ScrapeSelector)
				}
				if blog.LastScanned != nil {
					fmt.Printf("    Last scanned: %s\n", blog.LastScanned.Format("2006-01-02 15:04"))
				}
				fmt.Printf("    Entries: %d total, %s (%s)\n", stats.Total, formatUnreadCount(stats.Unread), formatInterestStats(stats))
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show feed URL and scrape selector")
	cmd.Flags().BoolVarP(&unreadOnly, "unread", "u", false, "Show only blogs with unread articles")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON document to stdout (for agentic use)")
	return cmd
}

type blogOverview struct {
	blog  model.Blog
	stats storage.ArticleStats
}

func loadBlogOverviews(db *storage.Database, blogs []model.Blog, unreadOnly bool) ([]blogOverview, error) {
	overviews := make([]blogOverview, 0, len(blogs))
	for _, blog := range blogs {
		stats, err := db.CountArticleStats(blog.ID)
		if err != nil {
			return nil, err
		}
		if unreadOnly && stats.Unread == 0 {
			continue
		}
		overviews = append(overviews, blogOverview{blog: blog, stats: stats})
	}
	return overviews, nil
}

func formatUnreadCount(unread int) string {
	text := fmt.Sprintf("%d unread", unread)
	if unread == 0 {
		return text
	}
	return color.New(color.FgYellow, color.Bold).Sprint(text)
}

func formatInterestStats(stats storage.ArticleStats) string {
	if stats.Unread == 0 {
		return "none h/n/p"
	}
	interestTotal := stats.Hide + stats.Normal + stats.Prefer
	if interestTotal == 0 {
		return "no interest data"
	}
	if interestTotal < stats.Unread {
		return "partial interest data"
	}
	return fmt.Sprintf("%d/%d/%d h/n/p", stats.Hide, stats.Normal, stats.Prefer)
}

func newExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export tracked blog definitions as a shell script.",
		Long: `Export tracked blog definitions as a POSIX shell script.

The output can be redirected to a file and run on another machine that has
blogwatcher installed, for example:

  blogwatcher export > blogs.sh
  sh blogs.sh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			script, err := controller.ExportBlogsScript(db)
			if err != nil {
				return err
			}
			fmt.Print(script)
			return nil
		},
	}
	return cmd
}

func newScanCommand() *cobra.Command {
	var silent bool
	var workers int
	var debugFlag bool
	var feedDiscovery bool

	cmd := &cobra.Command{
		Use:   "scan [blog_name]",
		Short: "Scan blogs for new articles (pre-fills summaries from RSS descriptions).",
		Long: `Scan blogs for new articles. Uses RSS/Atom feeds when available, otherwise
falls back to HTML scraping via the configured CSS selector.

For blogs that have a scrape selector but no feed URL, feed auto-discovery
is skipped by default to avoid slow probes against sites without RSS.
Use --feed-discovery to force feed discovery even when a selector is set.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dbg *debug.Logger
			if debugFlag {
				dbg = debug.New()
				dbg.Log("scan command started")
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				result, err := scanner.ScanBlogByNameDebug(db, args[0], feedDiscovery, dbg)
				if err != nil {
					return err
				}
				if result == nil {
					err := fmt.Errorf("Blog '%s' not found", args[0])
					printError(err)
					return markError(err)
				}
				if !silent {
					printScanResult(*result)
				}
			} else {
				blogs, err := db.ListBlogs()
				if err != nil {
					return err
				}
				if len(blogs) == 0 {
					fmt.Println("No blogs tracked yet. Use 'blogwatcher add' to add one.")
					return nil
				}
				if !silent {
					color.New(color.FgCyan).Printf("Scanning %d blog(s)...\n\n", len(blogs))
				}
				results, err := scanner.ScanAllBlogsDebug(db, workers, feedDiscovery, dbg)
				if err != nil {
					return err
				}
				totalNew := 0
				for _, result := range results {
					if !silent {
						printScanResult(result)
					}
					totalNew += result.NewArticles
				}
				if !silent {
					fmt.Println()
					if totalNew > 0 {
						color.New(color.FgGreen, color.Bold).Printf("Found %d new article(s) total!\n", totalNew)
					} else {
						color.New(color.FgYellow).Println("No new articles found.")
					}
				}
			}

			if silent {
				fmt.Println("scan done")
			}
			dbg.Log("scan command finished")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&silent, "silent", "s", false, "Only output 'scan done' when complete")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers when scanning all blogs")
	cmd.Flags().BoolVar(&feedDiscovery, "feed-discovery", false, "Try to discover RSS/Atom feeds even for blogs with a scrape selector")
	cmd.Flags().BoolVar(&debugFlag, "debug", false, "Show timestamped debug/profiling output on stderr")
	return cmd
}

func newArticlesCommand() *cobra.Command {
	var showAll bool
	var blogName string
	var showSummary bool
	var verbose bool
	var interestFilterValues []string
	var sortFlag string
	var sinceFlag string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "articles [article_id...]",
		Short: "List articles.",
		Long: `List articles. When article IDs are given, show only those articles.
Otherwise list unread articles (or all with --all).

The --filter flag controls interest-based filtering:
  all     no filtering (default)
  hide    show hide-classified only
  normal  show normal-classified and unclassified (alias: norm)
  prefer  show prefer-classified only (alias: pref)

Repeat --filter or pass a comma-separated list to combine filters.

Use --since with YYYY-MM-DD or a number of days to show posts on or after the
cutoff date. The filter uses published date, falling back to discovered date.

Use --json for a machine-readable list with full article fields and cached HN
data (when available) for agentic use.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sinceFlag != "" && len(args) > 0 {
				err := fmt.Errorf("cannot combine --since with article IDs")
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			interestFilter, err := controller.ParseInterestFilter(interestFilterValues)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			sortOrder, err := controller.ParseSortOrder(sortFlag)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			since, err := controller.ParseSince(sinceFlag, time.Now())
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			defer db.Close()

			var articles []model.Article
			var blogNames map[int64]string

			if len(args) > 0 {
				ids := make([]int64, 0, len(args))
				for _, arg := range args {
					id, err := parseID(arg)
					if err != nil {
						if jsonOut {
							return emitJSONError(err)
						}
						return err
					}
					ids = append(ids, id)
				}
				articles, blogNames, err = controller.GetArticlesByIDs(db, ids)
			} else {
				articles, blogNames, err = controller.GetArticlesByFilterSince(db, showAll, blogName, interestFilter, since)
			}
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				printError(err)
				return markError(err)
			}

			controller.SortArticles(articles, sortOrder)

			if jsonOut {
				out := make([]jsonArticle, 0, len(articles))
				for _, a := range articles {
					out = append(out, toJSONArticle(a, blogNames[a.BlogID]))
				}
				return emitJSON(struct {
					Articles []jsonArticle `json:"articles"`
				}{Articles: out})
			}

			if len(articles) == 0 {
				if len(args) > 0 {
					fmt.Println("No articles found.")
				} else if showAll {
					fmt.Println("No articles found.")
				} else {
					color.New(color.FgGreen).Println("No unread articles!")
				}
				return nil
			}

			if len(args) == 0 {
				label := "Unread articles"
				if showAll {
					label = "All articles"
				}
				color.New(color.FgCyan, color.Bold).Printf("%s (%d):\n\n", label, len(articles))
			}
			for _, article := range articles {
				printArticle(article, blogNames[article.BlogID], showSummary, verbose)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().BoolVarP(&showSummary, "summary", "s", false, "Show cached summary text alongside articles")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show blog, engine, summary size, and timestamp metadata")
	cmd.Flags().StringArrayVarP(&interestFilterValues, "filter", "f", []string{"all"}, "Interest filter: all, hide, normal/norm, prefer/pref (repeat or comma-separate)")
	cmd.Flags().StringVar(&sortFlag, "sort", "newest", "Sort by date: newest or oldest")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Show posts since YYYY-MM-DD or N days ago")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON document to stdout (for agentic use)")
	return cmd
}

func newReadCommand() *cobra.Command {
	var filterValues []string
	var blogName string
	var yes bool

	cmd := &cobra.Command{
		Use:   "read [article_id ...]",
		Short: "Mark articles as read by ID or by interest filter.",
		Long: `Mark one or more articles as read by ID, or mark all unread articles
matching an interest filter.

Examples:
  blogwatcher read 42
  blogwatcher read 42 99 101
  blogwatcher read --filter hide
  blogwatcher read --filter all --blog "Tech Blog" --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(filterValues) > 0 && len(args) > 0 {
				return fmt.Errorf("cannot combine --filter with article IDs")
			}
			if len(filterValues) == 0 && len(args) == 0 {
				return fmt.Errorf("provide article IDs or use --filter")
			}
			interestFilter, err := controller.ParseInterestFilter(filterValues)
			if err != nil {
				return err
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if len(filterValues) > 0 {
				return readByFilter(db, blogName, interestFilter, yes)
			}

			for _, arg := range args {
				articleID, err := parseID(arg)
				if err != nil {
					return err
				}
				article, err := controller.MarkArticleRead(db, articleID)
				if err != nil {
					printError(err)
					return markError(err)
				}
				if article.IsRead {
					fmt.Printf("Article %d is already marked as read.\n", articleID)
				} else {
					color.New(color.FgGreen).Printf("Marked article %d as read\n", articleID)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&filterValues, "filter", "f", nil, "Mark read by interest filter: all, hide, normal/norm, prefer/pref (repeat or comma-separate)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Only mark articles from this blog (with --filter)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt (with --filter)")
	return cmd
}

func readByFilter(db *storage.Database, blogName string, filter controller.InterestFilter, yes bool) error {
	allArticles, _, err := controller.GetArticles(db, false, blogName, "all")
	if err != nil {
		printError(err)
		return markError(err)
	}

	articles := make([]model.Article, 0, len(allArticles))
	for _, a := range allArticles {
		if filter.Match(a) {
			articles = append(articles, a)
		}
	}

	if !filter.Match(model.Article{}) {
		var unclassified int
		for _, a := range allArticles {
			if a.InterestState == "" {
				unclassified++
			}
		}
		if unclassified > 0 {
			interestCmd := "blogwatcher interest"
			if blogName != "" {
				interestCmd += fmt.Sprintf(" --blog %q", blogName)
			}
			color.New(color.FgYellow).Fprintf(os.Stderr,
				"Warning: %d unread article(s) have no interest classification and were skipped.\n"+
					"Run: %s\n", unclassified, interestCmd)
		}
	}

	if len(articles) == 0 {
		color.New(color.FgGreen).Println("No matching unread articles to mark as read.")
		return nil
	}

	if !yes {
		desc := fmt.Sprintf("filter=%s", filter.String())
		if blogName != "" {
			desc += fmt.Sprintf(", blog='%s'", blogName)
		}
		confirmed, err := confirm(fmt.Sprintf("Mark %d article(s) (%s) as read?", len(articles), desc))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	marked, err := controller.MarkArticlesReadByFilter(db, blogName, filter)
	if err != nil {
		printError(err)
		return markError(err)
	}

	color.New(color.FgGreen).Printf("Marked %d article(s) as read\n", len(marked))
	return nil
}

func newUnreadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unread <article_id>",
		Short: "Mark an article as unread.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			articleID, err := parseID(args[0])
			if err != nil {
				return err
			}
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()
			article, err := controller.MarkArticleUnread(db, articleID)
			if err != nil {
				printError(err)
				return markError(err)
			}
			if !article.IsRead {
				fmt.Printf("Article %d is already marked as unread.\n", articleID)
			} else {
				color.New(color.FgGreen).Printf("Marked article %d as unread\n", articleID)
			}
			return nil
		},
	}
	return cmd
}

func newSummaryCommand() *cobra.Command {
	var blogName string
	var showAll bool
	var interestFilterValues []string
	var forceExtractive bool
	var refresh bool
	var limit int
	var workers int
	var modelFlag string
	var verbose bool
	var debugFlag bool
	var sortFlag string
	var sinceFlag string
	var hackerNews bool
	var hackerNewsRefresh bool
	var hackerNewsLimit int
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "summary [article_id]",
		Short: "Summarize articles using AI or extractive fallback.",
		Long: `Summarize articles. If OPENAI_API_KEY is set, uses OpenAI for AI-powered summaries.
Otherwise, extracts the first ~2000 characters of article text (extractive mode).

Without arguments, summarizes all unread articles. With an article ID, summarizes that specific article.
Summaries are cached in the database for instant retrieval on repeat calls.

RSS-sourced summaries: when articles are discovered via RSS/Atom feeds, blogwatcher
automatically extracts the feed description and stores it as an initial summary
(engine = "rss"). Short RSS descriptions (under 500 characters, typical of feeds
like OpenAI or DeepMind) are automatically upgraded to full summaries on the next
summary or interest run — no --refresh needed. Longer RSS summaries (500+ chars)
are treated as cached and kept unless --refresh is used. If upgrading or refreshing
fails (e.g. HTTP 403), the existing RSS summary is always preserved.

Configuration via ~/.blogwatcher/config.toml:

  [summary]
  model = "gpt-5.4-nano"           # OpenAI model to use
  system_prompt = "..."            # Custom system prompt
  max_request_bytes = 40960        # Max article text sent to LLM (bytes)
  hackernews_max_request_bytes = 204800
  hackernews = false               # Enable --hn behavior without passing --hn
  hackernews_prompt = "..."        # Custom HN discussion summary prompt

HN enrichment uses cached HN summaries when present. Missing HN summaries are
generated only for selected articles and are capped by --hn-limit (default 30).
Use --hn-refresh to refresh HN metadata and regenerate HN summaries. Large HN
threads are truncated to hackernews_max_request_bytes before the LLM call.

Use --json for a machine-readable list of summary results (with engine, cached,
upgraded, warning, and HN data) for agentic use.

Use --filter to select articles by interest state before summarizing:
all, hide, normal/norm, prefer/pref. Repeat --filter or pass a comma-separated
list to combine filters.

Use --since with YYYY-MM-DD or a number of days to summarize posts on or after
the cutoff date. The filter uses published date, falling back to discovered date.

Estimated LLM cost per article (~10K input tokens, ~200 output tokens):

  gpt-4o-mini     ~$0.0015/article   (cheapest, older model)
  gpt-5-mini      ~$0.0029/article
  gpt-5.4-nano    ~$0.0023/article   (default, best value)
  gpt-5.4-mini    ~$0.0084/article   (strongest mini model)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dbg *debug.Logger
			if debugFlag {
				dbg = debug.New()
				dbg.Log("summary command started")
			}

			if sinceFlag != "" && len(args) > 0 {
				err := fmt.Errorf("cannot combine --since with article IDs")
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			sortOrder, err := controller.ParseSortOrder(sortFlag)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			interestFilter, err := controller.ParseInterestFilter(interestFilterValues)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			since, err := controller.ParseSince(sinceFlag, time.Now())
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				werr := fmt.Errorf("config: %v", err)
				if jsonOut {
					return emitJSONError(werr)
				}
				printError(werr)
				return markError(err)
			}
			opts := summarizer.OptionsFromConfig(cfg.Summary)
			if modelFlag != "" {
				opts.Model = modelFlag
			}
			if hackerNewsLimit < 0 {
				err := fmt.Errorf("--hn-limit must be 0 or greater")
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			hnOpts := controller.HackerNewsOptions{
				Enabled: hackerNews || hackerNewsRefresh || cfg.Summary.HackerNews,
				Refresh: hackerNewsRefresh,
				Limit:   hackerNewsLimit,
				Options: opts,
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				articleID, err := parseID(args[0])
				if err != nil {
					if jsonOut {
						return emitJSONError(err)
					}
					return err
				}
				result, err := controller.SummarizeArticle(db, articleID, forceExtractive, refresh, opts, hnOpts)
				if err != nil {
					if jsonOut {
						return emitJSONError(err)
					}
					printError(err)
					return markError(err)
				}
				if jsonOut {
					return emitJSON(struct {
						Summaries []jsonSummaryResult `json:"summaries"`
					}{Summaries: []jsonSummaryResult{toJSONSummaryResult(result)}})
				}
				printSummaryResult(result, verbose)
			} else {
				results, err := controller.SummarizeArticlesDebugByFilterSince(db, showAll, blogName, interestFilter, since, forceExtractive, refresh, limit, workers, opts, dbg, hnOpts)
				if err != nil {
					if jsonOut {
						return emitJSONError(err)
					}
					printError(err)
					return markError(err)
				}
				controller.SortSummaryResults(results, sortOrder)
				if jsonOut {
					out := make([]jsonSummaryResult, 0, len(results))
					for _, r := range results {
						out = append(out, toJSONSummaryResult(r))
					}
					return emitJSON(struct {
						Summaries []jsonSummaryResult `json:"summaries"`
					}{Summaries: out})
				}
				if len(results) == 0 {
					if showAll {
						fmt.Println("No articles found.")
					} else {
						color.New(color.FgGreen).Println("No unread articles to summarize!")
					}
					return nil
				}
				label := "Unread article summaries"
				if showAll {
					label = "All article summaries"
				}
				color.New(color.FgCyan, color.Bold).Printf("%s (%d):\n\n", label, len(results))
				for _, result := range results {
					printSummaryResult(result, verbose)
				}
			}
			dbg.Log("summary command finished")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Summarize all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().StringArrayVarP(&interestFilterValues, "filter", "f", []string{"all"}, "Interest filter: all, hide, normal/norm, prefer/pref (repeat or comma-separate)")
	cmd.Flags().BoolVarP(&forceExtractive, "extractive", "x", false, "Force extractive fallback (first ~2K chars, ignore OPENAI_API_KEY)")
	cmd.Flags().BoolVarP(&refresh, "refresh", "r", false, "Re-generate summary even if cached")
	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Max number of articles to summarize (safety limit for LLM costs)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers for parallel summarization")
	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "OpenAI model to use (overrides config)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show blog, engine, and summary size metadata")
	cmd.Flags().BoolVar(&debugFlag, "debug", false, "Show timestamped debug/profiling output on stderr")
	cmd.Flags().StringVar(&sortFlag, "sort", "newest", "Sort by date: newest or oldest")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Summarize posts since YYYY-MM-DD or N days ago")
	cmd.Flags().BoolVar(&hackerNews, "hn", false, "Add cached or missing Hacker News data for selected articles")
	cmd.Flags().BoolVar(&hackerNewsRefresh, "hn-refresh", false, "Refresh Hacker News data and regenerate HN summaries")
	cmd.Flags().IntVar(&hackerNewsLimit, "hn-limit", 30, "Max HN discussion summaries to generate in this run")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON document to stdout (for agentic use)")
	return cmd
}

func newInterestCommand() *cobra.Command {
	var blogName string
	var showAll bool
	var interestFilterValues []string
	var refresh bool
	var refreshSummary bool
	var forceExtractive bool
	var limit int
	var workers int
	var modelFlag string
	var verbose bool
	var showSummary bool
	var debugFlag bool
	var sortFlag string
	var sinceFlag string
	var hackerNews bool
	var hackerNewsRefresh bool
	var hackerNewsLimit int
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "interest [article_id]",
		Short: "Classify article interest using the cached summary.",
		Long: `Classify article interest as prefer, normal, or hide.

The classifier always uses the article summary as input. If a summary is missing,
blogwatcher generates and caches one first.

If interest_prompt and the per-blog override are both empty, articles are
left unclassified and no interest ranking is stored.

Example interest_prompt:

  Prefer technical depth, clear new information, or unusually actionable insight.
  Hide low-signal announcements, generic marketing, repetitive posts, and generic launch news.

Configuration via ~/.blogwatcher/config.toml:

  [interest]
  model = "gpt-5.4-nano"
  system_prompt = "..."
  max_request_bytes = 12288
  interest_prompt = "Prefer systems posts with concrete benchmarks and hide generic launch posts."

  [interest.blogs."Tech Blog"]
  interest_prompt = "Prefer compiler and database internals; hide AI hot takes and marketing."

Use --hn or [summary].hackernews=true to add cached or missing Hacker News data.
Missing HN summaries are capped by --hn-limit (default 30); --hn-refresh regenerates them.

Use --json for a machine-readable list of interest results (with engine, cached,
skipped, note, and HN data) for agentic use.

Use --filter to select articles by interest state before classifying:
all, hide, normal/norm, prefer/pref. Repeat --filter or pass a comma-separated
list to combine filters.

Use --since with YYYY-MM-DD or a number of days to classify posts on or after
the cutoff date. The filter uses published date, falling back to discovered date.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dbg *debug.Logger
			if debugFlag {
				dbg = debug.New()
				dbg.Log("interest command started")
			}

			if sinceFlag != "" && len(args) > 0 {
				err := fmt.Errorf("cannot combine --since with article IDs")
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			sortOrder, err := controller.ParseSortOrder(sortFlag)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			interestFilter, err := controller.ParseInterestFilter(interestFilterValues)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			since, err := controller.ParseSince(sinceFlag, time.Now())
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}

			cfg, err := config.Load()
			if err != nil {
				werr := fmt.Errorf("config: %v", err)
				if jsonOut {
					return emitJSONError(werr)
				}
				printError(werr)
				return markError(err)
			}

			summaryOpts := summarizer.OptionsFromConfig(cfg.Summary)
			interestCfg := cfg.Interest
			if modelFlag != "" {
				interestCfg.Model = modelFlag
			}
			if hackerNewsLimit < 0 {
				err := fmt.Errorf("--hn-limit must be 0 or greater")
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			hnOpts := controller.HackerNewsOptions{
				Enabled: hackerNews || hackerNewsRefresh || cfg.Summary.HackerNews,
				Refresh: hackerNewsRefresh,
				Limit:   hackerNewsLimit,
				Options: summaryOpts,
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				articleID, err := parseID(args[0])
				if err != nil {
					if jsonOut {
						return emitJSONError(err)
					}
					return err
				}
				result, err := controller.ClassifyArticleInterest(db, articleID, refresh, refreshSummary, forceExtractive, summaryOpts, interestCfg, hnOpts)
				if err != nil {
					if jsonOut {
						return emitJSONError(err)
					}
					printError(err)
					return markError(err)
				}
				if jsonOut {
					return emitJSON(struct {
						Interests []jsonInterestResult `json:"interests"`
					}{Interests: []jsonInterestResult{toJSONInterestResult(result)}})
				}
				printInterestResult(result, verbose, showSummary)
				return nil
			}

			results, err := controller.ClassifyArticlesInterestDebugByFilterSince(db, showAll, blogName, interestFilter, since, refresh, refreshSummary, forceExtractive, limit, workers, summaryOpts, interestCfg, dbg, hnOpts)
			if err != nil {
				if jsonOut {
					return emitJSONError(err)
				}
				printError(err)
				return markError(err)
			}
			controller.SortInterestResults(results, sortOrder)
			if jsonOut {
				out := make([]jsonInterestResult, 0, len(results))
				for _, r := range results {
					out = append(out, toJSONInterestResult(r))
				}
				return emitJSON(struct {
					Interests []jsonInterestResult `json:"interests"`
				}{Interests: out})
			}
			if len(results) == 0 {
				if showAll {
					fmt.Println("No articles found.")
				} else {
					color.New(color.FgGreen).Println("No unread articles to classify!")
				}
				return nil
			}

			label := "Unread article interest"
			if showAll {
				label = "All article interest"
			}
			color.New(color.FgCyan, color.Bold).Printf("%s (%d):\n\n", label, len(results))
			for _, result := range results {
				printInterestResult(result, verbose, showSummary)
			}
			dbg.Log("interest command finished")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Classify all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().StringArrayVarP(&interestFilterValues, "filter", "f", []string{"all"}, "Interest filter: all, hide, normal/norm, prefer/pref (repeat or comma-separate)")
	cmd.Flags().BoolVarP(&refresh, "refresh", "r", false, "Re-classify interest even if cached")
	cmd.Flags().BoolVar(&refreshSummary, "refresh-summary", false, "Re-generate summaries before classification")
	cmd.Flags().BoolVarP(&forceExtractive, "extractive", "x", false, "Force extractive fallback when generating missing summaries")
	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Max number of articles to classify (safety limit for LLM costs)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers for parallel classification")
	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "OpenAI model to use for interest classification (overrides config)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show blog, engine, summary size, and timestamp metadata")
	cmd.Flags().BoolVarP(&showSummary, "summary", "s", false, "Show cached summary text alongside interest results")
	cmd.Flags().BoolVar(&debugFlag, "debug", false, "Show timestamped debug/profiling output on stderr")
	cmd.Flags().StringVar(&sortFlag, "sort", "newest", "Sort by date: newest or oldest")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Classify posts since YYYY-MM-DD or N days ago")
	cmd.Flags().BoolVar(&hackerNews, "hn", false, "Add cached or missing Hacker News data for selected articles")
	cmd.Flags().BoolVar(&hackerNewsRefresh, "hn-refresh", false, "Refresh Hacker News data and regenerate HN summaries")
	cmd.Flags().IntVar(&hackerNewsLimit, "hn-limit", 30, "Max HN discussion summaries to generate in this run")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON document to stdout (for agentic use)")
	return cmd
}

func printSummaryResult(result controller.SummaryResult, verbose bool) {
	idStr := color.New(color.FgCyan).Sprintf("[%d]", result.Article.ID)
	interestTag := formatInterestTag(result.Article.InterestState)

	if interestTag != "" {
		fmt.Printf("  %s %s %s\n", idStr, interestTag, result.Article.Title)
	} else {
		fmt.Printf("  %s %s\n", idStr, result.Article.Title)
	}
	if verbose {
		fmt.Printf("       Blog: %s\n", result.BlogName)
	}
	fmt.Printf("       URL: %s\n", displayArticleURL(result.Article.URL))
	if result.Article.PublishedDate != nil {
		fmt.Printf("       Published: %s\n", result.Article.PublishedDate.Format("2006-01-02"))
	}
	if result.Article.InterestReason != "" {
		fmt.Printf("       Reason: %s\n", result.Article.InterestReason)
	}
	if result.Warning != "" {
		color.New(color.FgYellow).Printf("       Note: %s\n", result.Warning)
	}
	printHackerNewsResult(result.HackerNews, verbose)
	summarizerLabel := result.Engine
	if result.Cached {
		summarizerLabel += " (cached)"
	}
	if verbose {
		fmt.Printf("       Summarizer: %s\n", summarizerLabel)
	}
	if verbose && result.Article.Summary != "" {
		chars := len(result.Article.Summary)
		words := len(strings.Fields(result.Article.Summary))
		fmt.Printf("       Summary size: %d chars, %d words\n", chars, words)
	}
	if result.Article.Summary != "" {
		fmt.Printf("       Summary: %s\n", result.Article.Summary)
	} else {
		color.New(color.FgYellow).Printf("       Summary: (failed to generate)\n")
	}
	fmt.Println()
}

func printInterestResult(result controller.InterestResult, verbose bool, showSummary bool) {
	idStr := color.New(color.FgCyan).Sprintf("[%d]", result.Article.ID)
	interestTag := formatInterestTag(result.Article.InterestState)

	if result.Skipped {
		fmt.Printf("  %s %s\n", idStr, result.Article.Title)
	} else if interestTag != "" {
		fmt.Printf("  %s %s %s\n", idStr, interestTag, result.Article.Title)
	} else {
		fmt.Printf("  %s %s\n", idStr, result.Article.Title)
	}
	if verbose {
		fmt.Printf("       Blog: %s\n", result.BlogName)
	}
	fmt.Printf("       URL: %s\n", displayArticleURL(result.Article.URL))
	if result.Article.PublishedDate != nil {
		fmt.Printf("       Published: %s\n", result.Article.PublishedDate.Format("2006-01-02"))
	}
	if result.Skipped {
		fmt.Printf("       Interest: (not classified)\n")
	}
	if result.Note != "" {
		color.New(color.FgYellow).Printf("       Note: %s\n", result.Note)
	}
	if result.Article.InterestReason != "" {
		fmt.Printf("       Reason: %s\n", result.Article.InterestReason)
	}
	printHackerNewsResult(result.HackerNews, verbose)
	if verbose {
		classifierLabel := result.Engine
		if result.Cached {
			classifierLabel += " (cached)"
		}
		if classifierLabel != "" {
			fmt.Printf("       Classifier: %s\n", classifierLabel)
		}
		if result.Article.InterestJudged != nil {
			fmt.Printf("       Judged: %s\n", result.Article.InterestJudged.Format(time.RFC3339))
		}
	}
	if verbose && result.Article.SummaryEngine != "" {
		fmt.Printf("       Summarizer: %s\n", result.Article.SummaryEngine)
	}
	if verbose && result.Article.Summary != "" {
		chars := len(result.Article.Summary)
		words := len(strings.Fields(result.Article.Summary))
		fmt.Printf("       Summary size: %d chars, %d words\n", chars, words)
	}
	if showSummary && result.Article.Summary != "" {
		fmt.Printf("       Summary: %s\n", result.Article.Summary)
	}
	fmt.Println()
}

func printHackerNewsResult(result *hackernews.Result, verbose bool) {
	if result == nil {
		return
	}
	if result.NotFound {
		fmt.Printf("       HN: no discussion found\n")
		return
	}
	label := fmt.Sprintf("%s, %d points, %d comments", result.URL, result.Points, result.Comments)
	if result.Cached {
		label += " (cached)"
	}
	fmt.Printf("       HN: %s\n", label)
	if verbose && result.DiscussionSummary != "" {
		fmt.Printf("       HN summary: %s\n", result.DiscussionSummary)
	}
	if verbose && result.Warning != "" {
		color.New(color.FgYellow).Printf("       HN note: %s\n", result.Warning)
	}
}

func printScanResult(result scanner.ScanResult) {
	statusColor := color.FgWhite
	if result.NewArticles > 0 {
		statusColor = color.FgGreen
	}
	color.New(color.FgWhite, color.Bold).Printf("  %s\n", result.BlogName)
	if result.Error != "" {
		color.New(color.FgRed).Printf("    Error: %s\n", result.Error)
		return
	}
	if result.Source == "none" {
		color.New(color.FgYellow).Println("    No feed or scraper configured")
		return
	}
	sourceLabel := "HTML"
	if result.Source == "rss" {
		sourceLabel = "RSS"
	}
	fmt.Printf("    Source: %s | Total: %d | Unread: %d | ", sourceLabel, result.TotalArticles, result.UnreadArticles)
	color.New(statusColor).Printf("New: %d\n", result.NewArticles)
}

func printArticle(article model.Article, blogName string, showSummary bool, verbose bool) {
	status := color.New(color.FgYellow).Sprint("[new]")
	if article.IsRead {
		status = color.New(color.FgHiBlack).Sprint("[read]")
	}
	idStr := color.New(color.FgCyan).Sprintf("[%d]", article.ID)
	interestTag := formatInterestTag(article.InterestState)
	if interestTag != "" {
		fmt.Printf("  %s %s %s %s\n", idStr, status, interestTag, article.Title)
	} else {
		fmt.Printf("  %s %s %s\n", idStr, status, article.Title)
	}
	if verbose {
		fmt.Printf("       Blog: %s\n", blogName)
	}
	fmt.Printf("       URL: %s\n", displayArticleURL(article.URL))
	if article.PublishedDate != nil {
		fmt.Printf("       Published: %s\n", article.PublishedDate.Format("2006-01-02"))
	}
	if verbose && article.DiscoveredDate != nil {
		fmt.Printf("       Discovered: %s\n", article.DiscoveredDate.Format("2006-01-02 15:04"))
	}
	if verbose && article.InterestState != "" {
		classifierLabel := article.InterestEngine
		if classifierLabel == "" {
			classifierLabel = "unknown"
		}
		fmt.Printf("       Interest: %s (%s)\n", article.InterestState, classifierLabel)
		if article.InterestReason != "" {
			fmt.Printf("       Reason: %s\n", article.InterestReason)
		}
		if article.InterestJudged != nil {
			fmt.Printf("       Judged: %s\n", article.InterestJudged.Format(time.RFC3339))
		}
	}
	if verbose && article.Summary != "" {
		summarizerLabel := article.SummaryEngine
		if summarizerLabel == "" {
			summarizerLabel = "unknown"
		}
		fmt.Printf("       Summarizer: %s\n", summarizerLabel)
		chars := len(article.Summary)
		words := len(strings.Fields(article.Summary))
		fmt.Printf("       Summary size: %d chars, %d words\n", chars, words)
	}
	if showSummary && article.Summary != "" {
		fmt.Printf("       Summary: %s\n", article.Summary)
	}
	if verbose {
		printArticleHackerNews(article)
	}
	fmt.Println()
}

func printArticleHackerNews(article model.Article) {
	if article.HNFetched == nil {
		fmt.Printf("       HN: not yet checked\n")
		return
	}
	if article.HNItemID == 0 {
		fmt.Printf("       HN: no discussion found (checked %s)\n", article.HNFetched.Format("2006-01-02 15:04"))
		return
	}
	fmt.Printf("       HN: fetched %s, %d points, %d comments\n",
		article.HNFetched.Format("2006-01-02 15:04"), article.HNPoints, article.HNComments)
	fmt.Printf("       HN URL: %s\n", hackernews.ItemURL(article.HNItemID))
	if article.HNSummary != "" {
		chars := len(article.HNSummary)
		words := len(strings.Fields(article.HNSummary))
		fmt.Printf("       HN summary: %d chars, %d words\n", chars, words)
	} else {
		fmt.Printf("       HN summary: (none)\n")
	}
}

func formatInterestTag(state string) string {
	switch state {
	case model.InterestStatePrefer:
		return color.New(color.FgGreen, color.Bold).Sprint("[prefer]")
	case model.InterestStateNormal:
		return color.New(color.FgBlue).Sprint("[normal]")
	case model.InterestStateHide:
		return color.New(color.FgHiBlack).Sprint("[hide]")
	default:
		return ""
	}
}

func displayArticleURL(rawURL string) string {
	return strings.TrimSuffix(rawURL, "#atom-everything")
}

func printError(err error) {
	color.New(color.FgRed).Printf("Error: %s\n", err.Error())
}

func parseID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid article id: %s", value)
	}
	return parsed, nil
}

func confirm(prompt string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

func newSkillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Print the blogwatcher skill document (for agentic systems).",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(skill.Content)
			return nil
		},
	}
	return cmd
}

func init() {
	cobra.EnableCommandSorting = false
	cobra.AddTemplateFunc("now", func() string { return time.Now().Format(time.RFC3339) })
}

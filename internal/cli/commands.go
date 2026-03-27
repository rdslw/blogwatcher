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
	"github.com/rdslw/blogwatcher/internal/model"
	"github.com/rdslw/blogwatcher/internal/scanner"
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

func newBlogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blogs",
		Short: "List all tracked blogs.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()
			blogs, err := db.ListBlogs()
			if err != nil {
				return err
			}
			if len(blogs) == 0 {
				fmt.Println("No blogs tracked yet. Use 'blogwatcher add' to add one.")
				return nil
			}
			color.New(color.FgCyan, color.Bold).Printf("Tracked blogs (%d):\n\n", len(blogs))
			for _, blog := range blogs {
				color.New(color.FgWhite, color.Bold).Printf("  %s\n", blog.Name)
				fmt.Printf("    URL: %s\n", blog.URL)
				if blog.FeedURL != "" {
					fmt.Printf("    Feed: %s\n", blog.FeedURL)
				}
				if blog.ScrapeSelector != "" {
					fmt.Printf("    Selector: %s\n", blog.ScrapeSelector)
				}
				if blog.LastScanned != nil {
					fmt.Printf("    Last scanned: %s\n", blog.LastScanned.Format("2006-01-02 15:04"))
				}
				fmt.Println()
			}
			return nil
		},
	}
	return cmd
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

	cmd := &cobra.Command{
		Use:   "scan [blog_name]",
		Short: "Scan blogs for new articles.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				result, err := scanner.ScanBlogByName(db, args[0])
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
				results, err := scanner.ScanAllBlogs(db, workers)
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
			return nil
		},
	}
	cmd.Flags().BoolVarP(&silent, "silent", "s", false, "Only output 'scan done' when complete")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers when scanning all blogs")
	return cmd
}

func newArticlesCommand() *cobra.Command {
	var showAll bool
	var blogName string
	var showSummary bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "articles",
		Short: "List articles.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()
			articles, blogNames, err := controller.GetArticles(db, showAll, blogName)
			if err != nil {
				printError(err)
				return markError(err)
			}
			if len(articles) == 0 {
				if showAll {
					fmt.Println("No articles found.")
				} else {
					color.New(color.FgGreen).Println("No unread articles!")
				}
				return nil
			}

			label := "Unread articles"
			if showAll {
				label = "All articles"
			}
			color.New(color.FgCyan, color.Bold).Printf("%s (%d):\n\n", label, len(articles))
			for _, article := range articles {
				printArticle(article, blogNames[article.BlogID], showSummary, verbose)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().BoolVarP(&showSummary, "summary", "s", false, "Show cached summaries alongside articles")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show extra article metadata")
	return cmd
}

func newReadCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <article_id>",
		Short: "Mark an article as read.",
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
			return nil
		},
	}
	return cmd
}

func newReadAllCommand() *cobra.Command {
	var blogName string
	var yes bool

	cmd := &cobra.Command{
		Use:   "read-all",
		Short: "Mark all unread articles as read.",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			articles, blogNames, err := controller.GetArticles(db, false, blogName)
			if err != nil {
				printError(err)
				return markError(err)
			}
			if len(articles) == 0 {
				color.New(color.FgGreen).Println("No unread articles to mark as read.")
				return nil
			}

			if !yes {
				scope := "all blogs"
				if blogName != "" {
					scope = fmt.Sprintf("from '%s'", blogName)
				}
				confirmed, err := confirm(fmt.Sprintf("Mark %d article(s) %s as read?", len(articles), scope))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}

			marked, err := controller.MarkAllArticlesRead(db, blogName)
			if err != nil {
				printError(err)
				return markError(err)
			}

			_ = blogNames
			color.New(color.FgGreen).Printf("Marked %d article(s) as read\n", len(marked))
			return nil
		},
	}

	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Only mark articles from this blog")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
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
	var forceExtractive bool
	var refresh bool
	var limit int
	var workers int
	var modelFlag string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "summary [article_id]",
		Short: "Summarize articles using AI or extractive fallback.",
		Long: `Summarize articles. If OPENAI_API_KEY is set, uses OpenAI for AI-powered summaries.
Otherwise, extracts the first ~2000 characters of article text (extractive mode).

Without arguments, summarizes all unread articles. With an article ID, summarizes that specific article.
Summaries are cached in the database for instant retrieval on repeat calls.

Configuration via ~/.blogwatcher/config.toml:

  [summary]
  model = "gpt-5.4-nano"           # OpenAI model to use
  system_prompt = "..."            # Custom system prompt
  max_request_bytes = 40960        # Max article text sent to LLM (bytes)

Estimated LLM cost per article (~10K input tokens, ~200 output tokens):

  gpt-4o-mini     ~$0.0015/article   (cheapest, older model)
  gpt-5-mini      ~$0.0029/article
  gpt-5.4-nano    ~$0.0023/article   (default, best value)
  gpt-5.4-mini    ~$0.0084/article   (strongest mini model)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				printError(fmt.Errorf("config: %v", err))
				return markError(err)
			}
			opts := summarizer.OptionsFromConfig(cfg.Summary)
			if modelFlag != "" {
				opts.Model = modelFlag
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				articleID, err := parseID(args[0])
				if err != nil {
					return err
				}
				result, err := controller.SummarizeArticle(db, articleID, forceExtractive, refresh, opts)
				if err != nil {
					printError(err)
					return markError(err)
				}
				printSummaryResult(result, verbose)
			} else {
				results, err := controller.SummarizeArticles(db, showAll, blogName, forceExtractive, refresh, limit, workers, opts)
				if err != nil {
					printError(err)
					return markError(err)
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
				color.New(color.FgCyan, color.Bold).Printf("# %s (%d)\n\n", label, len(results))
				for _, result := range results {
					printSummaryResult(result, verbose)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Summarize all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().BoolVarP(&forceExtractive, "extractive", "x", false, "Force extractive fallback (first ~2K chars, ignore OPENAI_API_KEY)")
	cmd.Flags().BoolVarP(&refresh, "refresh", "r", false, "Re-generate summary even if cached")
	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Max number of articles to summarize (safety limit for LLM costs)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers for parallel summarization")
	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "OpenAI model to use (overrides config)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show engine and cache metadata")
	return cmd
}

func newInterestCommand() *cobra.Command {
	var blogName string
	var showAll bool
	var refresh bool
	var refreshSummary bool
	var forceExtractive bool
	var limit int
	var workers int
	var modelFlag string
	var verbose bool

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
  interest_prompt = "Prefer systems posts with concrete benchmarks and hide generic launch posts."

  [interest.blogs."Tech Blog"]
  interest_prompt = "Prefer compiler and database internals; hide AI hot takes and marketing."`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				printError(fmt.Errorf("config: %v", err))
				return markError(err)
			}

			summaryOpts := summarizer.OptionsFromConfig(cfg.Summary)
			interestCfg := cfg.Interest
			if modelFlag != "" {
				interestCfg.Model = modelFlag
			}

			db, err := storage.OpenDatabase("")
			if err != nil {
				return err
			}
			defer db.Close()

			if len(args) == 1 {
				articleID, err := parseID(args[0])
				if err != nil {
					return err
				}
				result, err := controller.ClassifyArticleInterest(db, articleID, refresh, refreshSummary, forceExtractive, summaryOpts, interestCfg)
				if err != nil {
					printError(err)
					return markError(err)
				}
				printInterestResult(result, verbose)
				return nil
			}

			results, err := controller.ClassifyArticlesInterest(db, showAll, blogName, refresh, refreshSummary, forceExtractive, limit, workers, summaryOpts, interestCfg)
			if err != nil {
				printError(err)
				return markError(err)
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
			color.New(color.FgCyan, color.Bold).Printf("# %s (%d)\n\n", label, len(results))
			for _, result := range results {
				printInterestResult(result, verbose)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Classify all articles (including read)")
	cmd.Flags().StringVarP(&blogName, "blog", "b", "", "Filter by blog name")
	cmd.Flags().BoolVarP(&refresh, "refresh", "r", false, "Re-classify interest even if cached")
	cmd.Flags().BoolVar(&refreshSummary, "refresh-summary", false, "Re-generate summaries before classification")
	cmd.Flags().BoolVarP(&forceExtractive, "extractive", "x", false, "Force extractive fallback when generating missing summaries")
	cmd.Flags().IntVarP(&limit, "limit", "l", 50, "Max number of articles to classify (safety limit for LLM costs)")
	cmd.Flags().IntVarP(&workers, "workers", "w", 8, "Number of concurrent workers for parallel classification")
	cmd.Flags().StringVarP(&modelFlag, "model", "m", "", "OpenAI model to use for interest classification (overrides config)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show engine, cache, and timestamp metadata")
	return cmd
}

func printSummaryResult(result controller.SummaryResult, verbose bool) {
	idStr := color.New(color.FgCyan).Sprintf("[%d]", result.Article.ID)

	fmt.Printf("## %s %s\n", idStr, result.Article.Title)
	fmt.Printf("- **Blog:** %s\n", result.BlogName)
	if result.Article.InterestState != "" {
		fmt.Printf("- **Interest:** %s\n", result.Article.InterestState)
		if result.Article.InterestReason != "" {
			fmt.Printf("- **Reason:** %s\n", result.Article.InterestReason)
		}
	}
	if result.Article.PublishedDate != nil {
		fmt.Printf("- **Published:** %s\n", result.Article.PublishedDate.Format("2006-01-02"))
	}
	if verbose {
		summarizerLabel := result.Engine
		if result.Cached {
			summarizerLabel += " (cached)"
		}
		fmt.Printf("- **Summarizer:** %s\n", summarizerLabel)
	}
	if result.Warning != "" {
		color.New(color.FgYellow).Printf("- **Note:** %s\n", result.Warning)
	}

	if result.Article.Summary != "" {
		fmt.Printf("- **Summary:** %s\n", result.Article.Summary)
	} else {
		color.New(color.FgYellow).Printf("- **Summary:** (failed to generate)\n")
	}
	fmt.Println()
}

func printInterestResult(result controller.InterestResult, verbose bool) {
	idStr := color.New(color.FgCyan).Sprintf("[%d]", result.Article.ID)

	fmt.Printf("## %s %s\n", idStr, result.Article.Title)
	fmt.Printf("- **Blog:** %s\n", result.BlogName)
	if result.Skipped {
		fmt.Printf("- **Interest:** (not classified)\n")
		if result.Note != "" {
			fmt.Printf("- **Note:** %s\n", result.Note)
		}
	} else {
		fmt.Printf("- **Interest:** %s\n", result.Article.InterestState)
	}
	if result.Article.InterestReason != "" {
		fmt.Printf("- **Reason:** %s\n", result.Article.InterestReason)
	}
	if result.Article.PublishedDate != nil {
		fmt.Printf("- **Published:** %s\n", result.Article.PublishedDate.Format("2006-01-02"))
	}
	if verbose {
		classifierLabel := result.Engine
		if result.Cached {
			classifierLabel += " (cached)"
		}
		if classifierLabel != "" {
			fmt.Printf("- **Classifier:** %s\n", classifierLabel)
		}
		if result.Article.InterestJudged != nil {
			fmt.Printf("- **Judged:** %s\n", result.Article.InterestJudged.Format(time.RFC3339))
		}
		if result.Article.SummaryEngine != "" {
			fmt.Printf("- **Summary Source:** %s\n", result.Article.SummaryEngine)
		}
	}
	fmt.Println()
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
	fmt.Printf("    Source: %s | Found: %d | ", sourceLabel, result.TotalFound)
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
	fmt.Printf("       URL: %s\n", displayArticleURL(article.URL))
	if verbose {
		fmt.Printf("       Blog: %s\n", blogName)
	}
	if article.PublishedDate != nil {
		fmt.Printf("       Published: %s\n", article.PublishedDate.Format("2006-01-02"))
	}
	if verbose && article.DiscoveredDate != nil {
		fmt.Printf("       Discovered: %s\n", article.DiscoveredDate.Format("2006-01-02 15:04"))
	}
	if verbose && article.Summary != "" {
		summarizerLabel := article.SummaryEngine
		if summarizerLabel == "" {
			summarizerLabel = "unknown"
		}
		fmt.Printf("       Summarizer: %s\n", summarizerLabel)
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
	if showSummary && article.Summary != "" {
		fmt.Printf("       Summary: %s\n", article.Summary)
	}
	fmt.Println()
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

func init() {
	cobra.EnableCommandSorting = false
	cobra.AddTemplateFunc("now", func() string { return time.Now().Format(time.RFC3339) })
}

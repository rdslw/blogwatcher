# BlogWatcher

A Go CLI tool to track blog articles, detect new posts, and manage read/unread status. Supports both RSS/Atom feeds and HTML scraping as fallback.

## Fork Notes

This repository is the `rdslw` fork of the original [`Hyaxia/blogwatcher`](https://github.com/Hyaxia/blogwatcher).

Short list of changes in this fork:

- **Article summaries**: adds cached article summarization with OpenAI-backed and local extractive modes.
- **Interest classification**: adds configurable `prefer` / `normal` / `hide` ranking driven by cached summaries.
- **Export command**: adds `blogwatcher export` to dump tracked blogs as a replayable shell script.
- Scraper parsing is more robust for tricky titles and published-date extraction on HTML-only blogs.

## Features

-   **Dual Source Support** - Tries RSS feeds first, falls back to HTML scraping
-   **Automatic Feed Discovery** - Detects RSS/Atom URLs from blog homepages
-   **Read/Unread Management** - Track which articles you've read
-   **Blog Filtering** - View articles from specific blogs
-   **Article Summaries** - Generate and cache summaries with OpenAI or local fallback modes
-   **Interest Classification** - Label articles as `prefer`, `normal`, or `hide` from their summaries
-   **Duplicate Prevention** - Never tracks the same article twice
-   **Colored CLI Output** - User-friendly terminal interface

## Installation

```bash
# Install the CLI
go install github.com/rdslw/blogwatcher/cmd/blogwatcher@latest

# Or build locally
go build ./cmd/blogwatcher

# Or use the bundled build targets
make build
```

Linux and macOS binaries are also available on the GitHub Releases page.

## Usage

### Adding Blogs

```bash
# Add a blog (auto-discovers RSS feed)
blogwatcher add "My Favorite Blog" https://example.com/blog

# Add with explicit feed URL
blogwatcher add "Tech Blog" https://techblog.com --feed-url https://techblog.com/rss.xml

# Add with HTML scraping selector (for blogs without feeds)
blogwatcher add "No-RSS Blog" https://norss.com --scrape-selector "article h2 a"
```

### Managing Blogs

```bash
# List all tracked blogs
blogwatcher blogs

# Remove a blog (and all its articles)
blogwatcher remove "My Favorite Blog"

# Remove without confirmation
blogwatcher remove "My Favorite Blog" -y

# Export blog definitions as a shell script for another machine
blogwatcher export > blogs.sh
sh blogs.sh
```

### Scanning for New Articles

```bash
# Scan all blogs for new articles
blogwatcher scan

# Scan a specific blog
blogwatcher scan "Tech Blog"
```

### Viewing Articles

```bash
# List unread articles
blogwatcher articles

# List all articles (including read)
blogwatcher articles --all

# List articles from a specific blog
blogwatcher articles --blog "Tech Blog"

# Show specific articles by ID
blogwatcher articles 42 99

# Filter by interest: all (default), norm (prefer+normal), prefer
blogwatcher articles --filter norm
blogwatcher articles -f prefer

# Show extra article metadata
blogwatcher articles --verbose

# Show cached summaries alongside articles
blogwatcher articles --summary

# Interest tags appear inline once classified
blogwatcher articles
```

### Summaries

```bash
# Summarize unread articles
blogwatcher summary

# Summarize all articles for a blog
blogwatcher summary --all --blog "Tech Blog"

# Refresh cached summaries
blogwatcher summary --refresh

# Force local non-LLM summarization
blogwatcher summary --extractive

# Show summarizer metadata
blogwatcher summary --verbose
```

### Interest Classification

```bash
# Classify unread articles
blogwatcher interest

# Classify one article
blogwatcher interest 42

# Re-classify existing labels
blogwatcher interest --refresh

# Rebuild summaries before classification
blogwatcher interest --refresh-summary

# Classify all articles for one blog
blogwatcher interest --all --blog "Tech Blog"
```

### Summary Configuration

Create `~/.blogwatcher/config.toml`:

```toml
[summary]
model = "gpt-5.4-nano"
openai_api_key = "sk-..."
max_request_bytes = 40960
system_prompt = """
You are a concise blog article summarizer. Summarize the following article text in 100 to 400 words.
Focus on the key points, main arguments, and conclusions.
Ignore navigation, cookie/privacy/legal notices, login or registration prompts,
subscription/paywall prompts, social-sharing UI, ads, and related/recent article lists if they appear in the text.
Use clear, informative language. Output only the summary text.
Use the same language as the blog article.
"""
```

### Interest Configuration

Create `~/.blogwatcher/config.toml` with a default interest prompt and optional per-blog overrides:

```toml
[interest]
openai_api_key = "sk-..."
model = "gpt-5.4-nano"
system_prompt = """
You are classifying whether a blog article is worth prioritizing for the user.
Return strict JSON with keys "state" and "reason".
Allowed states are "prefer", "normal", and "hide".
"""
interest_prompt = """
Prefer technical writeups with concrete details, benchmarks, architectural lessons,
or clear implementation tradeoffs.
Hide generic product launches, funding news, AI hot takes, and obvious marketing posts.
"""
```

```toml
# Optional per-blog override. If present, this replaces interest_prompt for that blog.
[interest.blogs."Tech Blog"]
interest_prompt = """
Prefer compiler, databases, and distributed systems posts with benchmarks or implementation details.
Hide generic AI hot takes, launch posts, hiring announcements, and broad opinion pieces.
"""
```

Interest classification always uses the cached article summary as input. If a summary
is missing, BlogWatcher generates and stores one first.

`interest_prompt` is optional. If `config.toml` is empty or the field is omitted,
BlogWatcher keeps `interest_prompt` empty and leaves articles unclassified, so no
interest ranking is created unless you define either `interest.interest_prompt` or a
blog-specific override.

Example `interest_prompt` you can start from:

```toml
[interest]
interest_prompt = """
Prefer technical depth, clear new information, or unusually actionable insight.
Hide low-signal announcements, generic marketing, repetitive posts, and generic launch news.
"""
```

Prompt writing tips:

- `prefer` examples: "Prefer posts with benchmarks, architecture diagrams, implementation details, incident writeups, or concrete tradeoff analysis."
- `hide` examples: "Hide launch announcements, release notes without substance, marketing content, funding news, link roundups, and repetitive opinion posts."

### Managing Read Status

```bash
# Mark an article as read (use article ID from articles list)
blogwatcher read 42

# Mark multiple articles as read
blogwatcher read 42 99 101

# Mark an article as unread
blogwatcher unread 42

# Mark all unread articles as read (by interest scope)
blogwatcher read --scope all

# Mark all "hide" articles as read
blogwatcher read --scope hide

# Mark all "normal" articles as read for a blog (skip prompt)
blogwatcher read --scope normal --blog "Tech Blog" --yes

# Mark all "prefer" articles as read
blogwatcher read --scope prefer
```

## How It Works

### Scanning Process

1. For each tracked blog, BlogWatcher first attempts to parse the RSS/Atom feed
2. If no feed URL is configured, it tries to auto-discover one from the blog homepage
3. If RSS parsing fails and a `scrape_selector` is configured, it falls back to HTML scraping
4. New articles are saved to the database as unread
5. Already-tracked articles are skipped

### Feed Auto-Discovery

BlogWatcher searches for feeds in two ways:

-   Looking for `<link rel="alternate">` tags with RSS/Atom types
-   Checking common feed paths: `/feed`, `/rss`, `/feed.xml`, `/atom.xml`, etc.

### HTML Scraping

When RSS isn't available, provide a CSS selector that matches article links:

```bash
# Example selectors
--scrape-selector "article h2 a"      # Links inside article h2 tags
--scrape-selector ".post-title a"     # Links with post-title class
--scrape-selector "#blog-posts a"     # Links inside blog-posts ID
```

## Database

BlogWatcher stores data in SQLite at `~/.blogwatcher/blogwatcher.db`:

-   **blogs** - Tracked blogs (name, URL, feed URL, scrape selector)
-   **articles** - Discovered articles (title, URL, dates, read status, cached summaries, summary engine, cached interest state/reason)

## Development

### Requirements

-   Go 1.24+

### Running Tests

```bash
# Run all tests
make test
```

### Building

```bash
# Run the test suite
make test

# Build for the current machine
make build

# Build the Linux release binary
make build-linux-amd64

# Cross-compile the macOS Apple Silicon binary from Linux
make build-macos

# Build both release artifacts into dist/
make release
```

By default the build version is derived from `git describe`. You can override it explicitly when needed:

```bash
VERSION=v1.2.3 make build-macos-arm64
```

### Publishing

Push a version tag to trigger the GoReleaser workflow on GitHub Actions:
```
  git tag vX.Y.Z
  git push origin vX.Y.Z
```

## License

MIT

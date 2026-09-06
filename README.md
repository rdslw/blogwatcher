# BlogWatcher

A Go CLI tool to track blog articles, detect new posts, and manage read/unread status. Supports both RSS/Atom feeds and HTML scraping as fallback.

## Fork Notes

This repository is the `rdslw` fork of the original [`Hyaxia/blogwatcher`](https://github.com/Hyaxia/blogwatcher).

Short list of changes in this fork:

- **Article summaries**: adds cached article summarization with OpenAI-backed and local extractive modes.
- **Interest classification**: adds configurable `prefer` / `normal` / `hide` ranking driven by cached summaries.
- **Hacker News enrichment**: optionally finds matching HN submissions and caches points, comment count, and an LLM summary of the discussion.
- **HN cost controls**: HN enrichment has its own cache, refresh flag, request-size limit, and `--hn-limit` guard for large runs.
- **Export command**: adds `blogwatcher export` to dump tracked blogs as a replayable shell script.
- **Rename command**: adds `blogwatcher rename <old-name> <new-name>` while preserving the blog's articles and cached data.
- **JSON output**: `--json` on `blogs`, `articles`, `summary`, and `interest` for scriptable / agentic consumers.
- **Time-bounded queries**: `--since` on `articles`, `summary`, and `interest` to select posts by date or recent day count.
- **Configurable User-Agent**: uses a versioned BlogWatcher identity by default, with a global `config.toml` override for feed, blog, and article requests.
- Scraper parsing is more robust for tricky titles and published-date extraction on HTML-only blogs.

### Upstream Integration Policy

As of September 6, 2026, we use **selective upstream imports** to preserve the fork's behavior. `origin/main` is our development and release branch; `upstream/main` is a reference for review.

- Import on `upstream/<topic>` branches from our `main`, using `git cherry-pick -x` or focused ports citing upstream PRs and hashes.
- Record review outcomes, reasons, and the reviewed SHA. Cherry-picks do not establish shared ancestry, so divergence counts alone cannot track outstanding work.
- Keep published history stable; avoid regular upstream merges, rebasing published `main`, and artificial ancestry merges.

Revisit this policy explicitly if our needs change.

## Features (original, pre-fork)

-   **Dual Source Support** - Tries RSS feeds first, falls back to HTML scraping
-   **RSS Summaries** - Automatically pre-fills article summaries from RSS feed descriptions during scan
-   **Automatic Feed Discovery** - Detects RSS/Atom URLs from blog homepages
-   **Read/Unread Management** - Track which articles you've read
-   **Blog Filtering** - View articles from specific blogs
-   **Article Summaries** - Generate and cache summaries with OpenAI or local fallback modes
-   **Interest Classification** - Label articles as `prefer`, `normal`, or `hide` from their summaries
-   **Skill Document** - Built-in `skill` command emits a machine-readable skill doc for agentic systems
-   **JSON Output** - `--json` on `blogs`, `articles`, `summary`, and `interest` for scriptable / agentic use
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

# Include feed URL and scrape selector
blogwatcher blogs -v

# Show only blogs that currently have unread articles
blogwatcher blogs --unread

# Rename a blog without losing its articles or cached data
blogwatcher rename "My Favorite Blog" "Favorite Blog"

# Remove a blog (and all its articles)
blogwatcher remove "My Favorite Blog"

# Remove without confirmation
blogwatcher remove "My Favorite Blog" -y

# Export blog definitions as a shell script for another machine
blogwatcher export > blogs.sh
sh blogs.sh
```

`Entries` interest labels apply to unread articles: `a/b/c h/n/p`, `none h/n/p`, `no interest data`, or `partial interest data`.

`blogs --unread` filters the blog list by whether each blog has unread articles.
It is intentionally different from `--filter` on commands such as `articles`,
`summary`, and `interest`, where the filter selects interest states (`hide`,
`normal`, or `prefer`).

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

# List posts since a date, or since N days ago
blogwatcher articles --since 2026-05-01
blogwatcher articles --since 7

# Show specific articles by ID
blogwatcher articles 42 99

# Filter by interest: all (default), hide, normal/norm, prefer/pref
blogwatcher articles --filter norm
blogwatcher articles -f pref
blogwatcher articles -f prefer,norm
blogwatcher articles -f prefer -f normal

# Show blog, engine, summary size, and timestamp metadata
blogwatcher articles --verbose

# Show cached summary text alongside articles
blogwatcher articles --summary

# Combine verbose metadata with summary text
blogwatcher articles -v -s

# Sort by date: newest first (default) or oldest first
blogwatcher articles --sort oldest

# Interest tags appear inline once classified
blogwatcher articles
```

### Summaries

```bash
# Summarize unread articles
blogwatcher summary

# Summarize all articles for a blog
blogwatcher summary --all --blog "Tech Blog"

# Summarize only prefer-classified articles
blogwatcher summary --filter pref

# Summarize posts since a date, or since N days ago
blogwatcher summary --since 2026-05-01
blogwatcher summary --since 7

# Refresh cached summaries
blogwatcher summary --refresh

# Force local non-LLM summarization
blogwatcher summary --extractive

# Show blog, engine, and summary size metadata
blogwatcher summary --verbose

# Add cached or missing Hacker News data when a matching submission exists
blogwatcher summary --hn

# With --verbose, also show an LLM summary of the HN discussion
blogwatcher summary --hn --verbose

# Regenerate HN metadata and discussion summaries
blogwatcher summary --hn-refresh

# Allow more than the default 30 new HN discussion summaries
blogwatcher summary --hn --hn-limit 100

# Sort output by date: newest first (default) or oldest first
blogwatcher summary --sort oldest
```

```bash
# RSS summaries are pre-filled during scan.
# Short ones (<500 chars) are auto-upgraded on the next summary/interest run.
# Longer ones are treated as cached; use --refresh to upgrade them.
# If upgrading fails (e.g. 403), the RSS summary is always preserved.
blogwatcher summary --refresh
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

# Classify normal-classified and unclassified articles
blogwatcher interest --filter norm

# Classify posts since a date, or since N days ago
blogwatcher interest --since 2026-05-01
blogwatcher interest --since 7

# Show cached summary text alongside interest results
blogwatcher interest --summary

# Show blog, engine, summary size, and timestamp metadata
blogwatcher interest --verbose

# Add cached or missing Hacker News data for articles processed by this run
blogwatcher interest --hn

# Regenerate HN data while classifying interest
blogwatcher interest --hn-refresh --hn-limit 100

# Sort output by date: newest first (default) or oldest first
blogwatcher interest --sort oldest
```

### Summary Configuration

Create `~/.blogwatcher/config.toml`:

```toml
# Optional. When omitted, BlogWatcher sends its versioned application User-Agent.
user_agent = "blogwatcher/v1.2.3 (+https://github.com/rdslw/blogwatcher)"

[summary]
model = "gpt-5.4-nano"
openai_api_key = "sk-..."
max_request_bytes = 40960
hackernews_max_request_bytes = 204800
hackernews = false
system_prompt = """
You are a concise blog article summarizer. Summarize the following article text in 100 to 400 words.
Focus on the key points, main arguments, and conclusions.
Ignore navigation, cookie/privacy/legal notices, login or registration prompts,
subscription/paywall prompts, social-sharing UI, ads, and related/recent article lists if they appear in the text.
Use clear, informative language. Output only the summary text.
Use the same language as the blog article.
"""
# Optional: hackernews_prompt can override the built-in Path ID discussion prompt.
```

The top-level `user_agent` applies to feed discovery, RSS/Atom fetching, HTML
scraping, and article-page fetching for summaries. If it is omitted or empty,
BlogWatcher uses `blogwatcher/<version> (+https://github.com/rdslw/blogwatcher)`.

HN enrichment stores `hn_item_id`, `hn_points`, `hn_comments`, `hn_summary`, and
`hn_fetched` in the article cache. `--hn` reuses cached HN summaries and generates
only missing ones. `--hn-refresh` fetches HN again and regenerates the HN summary.
If no HN discussion is found, `hn_item_id = 0` and `hn_fetched` record the check,
but later `--hn` runs still retry because articles can reach HN later.
New HN summaries are capped by `--hn-limit` (default 30); raise it explicitly for
large runs such as `--all`. Large HN discussions are truncated to
`hackernews_max_request_bytes` before the LLM call; verbose output notes truncation.
The summary and HN limits are separate: `max_request_bytes` is for article text,
while `hackernews_max_request_bytes` is for HN Path ID discussion text.

### Interest Configuration

Create `~/.blogwatcher/config.toml` with a default interest prompt and optional per-blog overrides:

```toml
[interest]
openai_api_key = "sk-..."
model = "gpt-5.4-nano"
max_request_bytes = 12288
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

When renaming a blog, also rename its `[interest.blogs."..."]` table if one
exists. The `rename` command preserves database records but does not edit
`~/.blogwatcher/config.toml`.

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

### JSON Output (for scripts / agents)

`blogs`, `articles`, `summary`, and `interest` accept `--json` and emit a single JSON document on stdout. No colors or headers are mixed in.

```bash
blogwatcher blogs --json
blogwatcher articles --json
blogwatcher articles --filter prefer --json
blogwatcher articles --since 7 --json
blogwatcher summary --filter pref --json
blogwatcher interest --filter norm --json
```

Envelope shapes:

- `blogs` → `{"blogs": [ { id, name, url, feed_url?, scrape_selector?, last_scanned?, stats: {total, unread, hide, normal, prefer} } ]}`
- `articles` → `{"articles": [ <article> ]}`
- `summary` → `{"summaries": [ { article: <article>, blog_name?, engine?, cached?, upgraded?, warning?, hn? } ]}`
- `interest` → `{"interests": [ { article: <article>, blog_name?, engine?, cached?, skipped?, note?, hn? } ]}`

`<article>` always includes `id`, `blog_id`, `title`, `url`, `is_read`, and (when populated) `blog_name`, `published_date`, `discovered_date`, `summary`, `summary_engine`, `interest_state`, `interest_reason`, `interest_engine`, `interest_judged`, `hn`. The `hn` object is included only when the article has been HN-checked and exposes `fetched`, `found`, and (when found) `item_id`, `url`, `points`, `comments`, `summary`, `warning`, `cached`.

On failure the command writes `{"error": "..."}` to stdout and exits non-zero.

### Skill Document

```bash
# Print the built-in skill document (for agentic systems / LLM tools)
blogwatcher skill
```

The skill document describes blogwatcher's commands, summary and interest pipelines,
standard workflow, and presentation conventions — designed for consumption by AI agents.
It is embedded in the binary at build time from `SKILL.md`.

### Managing Read Status

```bash
# Mark an article as read (use article ID from articles list)
blogwatcher read 42

# Mark multiple articles as read
blogwatcher read 42 99 101

# Mark an article as unread
blogwatcher unread 42

# Mark all unread articles as read
blogwatcher read --filter all

# Mark all "hide" articles as read
blogwatcher read --filter hide

# Mark all "normal" and unclassified articles as read for a blog (skip prompt)
blogwatcher read --filter norm --blog "Tech Blog" --yes

# Mark all "prefer" articles as read
blogwatcher read --filter pref
```

## How It Works

### Scanning Process

1. For each tracked blog, BlogWatcher first attempts to parse the RSS/Atom feed
2. If no feed URL is configured, it tries to auto-discover one from the blog homepage
3. If RSS parsing fails and a `scrape_selector` is configured, it falls back to HTML scraping
4. New articles are saved to the database as unread. If the RSS feed includes a description, it is stored as an initial summary (engine = "rss", up to 2000 characters)
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

# Build for the current machine as ./blogwatcher
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
VERSION=v1.2.3 make build-macos
```

### Publishing

To publish a release:
```
  # Update internal/version/version.go to the release version
  git commit -m "ver: release vX.Y.Z" -- internal/version/version.go
  git tag vX.Y.Z
  git push origin main vX.Y.Z
```

## License

MIT

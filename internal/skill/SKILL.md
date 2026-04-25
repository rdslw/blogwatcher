---
name: blogwatcher
description: Monitor blogs, websites and RSS/Atom feeds for new articles, summarize them with AI, and classify interest — using the blogwatcher CLI.
---

# blogwatcher — Blog & Feed Monitor

Binary: `blogwatcher` (Go CLI, expected in PATH)
Config: `~/.blogwatcher/config.toml`
Database: `~/.blogwatcher/blogwatcher.db` (SQLite, created on demand)

## Commands

| Command | Purpose |
|---------|---------|
| `blogwatcher scan` | Scan all blogs for new articles |
| `blogwatcher scan <name>` | Scan a single blog |
| `blogwatcher blogs` | List tracked blogs with URLs and last scan time |
| `blogwatcher articles` | List unread articles |
| `blogwatcher articles <id> [id...]` | Show specific articles by ID |
| `blogwatcher articles --all` | All articles (including read) |
| `blogwatcher articles --blog <name>` | Unread articles for one blog |
| `blogwatcher articles --filter norm` | Hide `hide`-classified, show prefer+normal |
| `blogwatcher articles --filter prefer` | Show only `prefer`-classified articles |
| `blogwatcher articles --sort oldest` | Order by date (earliest first; default is `newest`) |
| `blogwatcher articles -s` | Show cached summary text alongside articles |
| `blogwatcher articles -v` | Show blog, engine, summary size, and timestamp metadata |
| `blogwatcher read <id> [id...]` | Mark article(s) as read |
| `blogwatcher read --scope hide` | Mark all hide-classified unread articles as read |
| `blogwatcher read --scope all` | Mark all unread articles as read |
| `blogwatcher unread <id>` | Mark article back to unread |
| `blogwatcher summary [id]` | Summarize article(s) (AI or extractive fallback) |
| `blogwatcher summary --all` | Summarize all articles including read |
| `blogwatcher summary --refresh` | Re-generate even if cached |
| `blogwatcher summary --extractive` | Force non-LLM snippet mode |
| `blogwatcher summary --sort oldest` | Order output by date (earliest first; default `newest`) |
| `blogwatcher interest [id]` | Classify article(s) as prefer/normal/hide |
| `blogwatcher interest --all` | Classify all articles including read |
| `blogwatcher interest --refresh` | Re-classify even if cached |
| `blogwatcher interest -s` | Show cached summary text alongside interest results |
| `blogwatcher interest -v` | Show blog, engine, summary size, and timestamp metadata |
| `blogwatcher interest --sort oldest` | Order output by date (earliest first; default `newest`) |
| `blogwatcher add <name> <url>` | Add blog (auto-discovers RSS) |
| `blogwatcher add <name> <url> --feed-url <rss>` | Add blog with explicit feed URL |
| `blogwatcher add <name> <url> --scrape-selector <css>` | Add blog with HTML scraping |
| `blogwatcher remove <name>` | Remove blog and its articles |
| `blogwatcher export` | Export blog definitions as portable shell script |
| `blogwatcher skill` | Print this skill document |

Common flags for `summary` and `interest`: `--blog <name>`, `--limit N`, `--workers N`, `--model <model>`, `--verbose`
Common flags for `articles` and `interest`: `--summary` (show cached summary text)
Common flags for `scan`, `summary`, and `interest`: `--debug` (timestamped profiling output on stderr)
Scan-specific: `--feed-discovery` (try RSS/Atom discovery even for blogs with a scrape selector)

## Summary Pipeline

The `summary` command generates a short text summary for each article.

**Flow:** fetch article HTML → strip boilerplate → extract main content → summarize via LLM (or snippet fallback).

- **Input:** article URL.
- **Content extraction:** fetches the page, removes nav/header/footer/ads/popups, then scores candidate content blocks (preferring `.post-content`, `article`, etc.) to find the main text.
- **Short articles** (under 250 words): stored verbatim, engine = `verbatim`.
- **LLM mode** (default when API key is set): sends extracted text to OpenAI chat completions. Engine = `openai`.
- **Snippet mode** (no API key, or `--extractive`): first ~2000 characters, truncated at sentence boundary. Engine = `snippet`.
- **Fallback:** if LLM call fails, falls back to snippet automatically.
- **Caching:** summaries are stored in the database. Subsequent calls return the cached version unless `--refresh` is used.
- **RSS summaries:** during `scan`, if an RSS/Atom feed item includes a description, it is stripped of HTML and stored as an initial summary (engine = `rss`, up to 2000 characters). Short RSS descriptions (under 500 characters) are automatically upgraded to full summaries on the next `summary` or `interest` run — no `--refresh` needed. Longer RSS summaries (500+ chars) are treated as cached and kept unless `--refresh` is used. If upgrading or refreshing fails (e.g. HTTP 403), the existing RSS summary is always preserved.
- **Cost control:** `--limit N` (default 50) caps how many articles are summarized per invocation. `--workers N` controls concurrency.

### Summary Configuration

```toml
[summary]
openai_api_key = "sk-..."       # or set OPENAI_API_KEY env var
model = "gpt-5.4-nano"          # default model
system_prompt = "..."            # custom summarizer prompt
max_request_bytes = 40960        # max article text sent to LLM
```

## Interest Classification Pipeline

The `interest` command classifies each article as **prefer**, **normal**, or **hide** based on its summary.

**Flow:** ensure summary exists → build prompt with blog name + classification policy + summary → LLM returns JSON `{"state": "...", "reason": "..."}`.

- **Dependency:** interest classification always requires a summary. If missing, `interest` auto-generates one first.
- **Labels:**
  - `prefer` — high signal, surface to user
  - `normal` — worth noting, not urgent
  - `hide` — low signal, skip or auto-mark read
- **Per-blog prompts:** each blog can have its own `interest_prompt` in config. Falls back to the global prompt. If no prompt exists for a blog, articles are left unclassified.
- **Caching:** classification is stored in the database with a timestamp. Re-run with `--refresh` to reclassify.
- **Output:** JSON parsed from LLM response. Invalid JSON or states are rejected and the article is left unclassified.

### Interest Configuration

```toml
[interest]
openai_api_key = "sk-..."
model = "gpt-5.4-nano"
system_prompt = "..."                # classifier system prompt (controls JSON output format)
interest_prompt = "Prefer ..."       # global classification policy

[interest.blogs."simonwillison"]
interest_prompt = "Prefer LLM tooling and Datasette posts; hide generic link roundups."

[interest.blogs."macrumors"]
interest_prompt = "Prefer Apple hardware releases; hide accessory reviews and rumors."
```

The `interest_prompt` is the user-facing classification policy — what matters to you. The `system_prompt` controls the LLM's output format and should rarely need changing.

## Standard Workflow

### 1. Scan for new articles

```
blogwatcher scan
```

Wait for completion. This fetches new articles from all tracked blogs via RSS/Atom or HTML scraping. RSS feed descriptions are automatically stored as initial summaries.

### 2. Classify interest (auto-summarizes)

```
blogwatcher interest -v
```

This generates summaries for articles that lack one, then classifies all unread articles. The `-v` flag shows engine and cache metadata.

### 3. Review articles

```
blogwatcher articles -f norm -v      # unread, excluding hide-classified
blogwatcher articles -f prefer -v    # prefer-only
blogwatcher articles -v -s           # unread with summaries
blogwatcher articles -v -s 42 99     # specific articles with summaries
blogwatcher interest -v -s           # interest results with summary text
```

**IMPORTANT:** Always copy URLs exactly from `blogwatcher articles` output. Never reconstruct or guess URLs.

### 4. Clean up

```
blogwatcher read --scope hide -y     # mark all hide-classified as read
blogwatcher read 42 99               # mark specific articles as read
```

## Presenting Articles to Users

Group articles by blog. Use interest labels and summaries for context.

```
📰 Blogwatcher — N new articles

⭐ **blogname** (2 prefer, 1 normal):
• [prefer] Article Title — https://example.com/...
  Two-sentence summary from cached data.
• [normal] Another Article — https://example.com/...
  Summary here.

**otherblog** (1 prefer):
• [prefer] Some Post — https://other.com/...

remaining-blogs — no updates.
```

- Add ⭐ prefix for blogs with prefer-classified articles.
- Include published date and interest reason when relevant.
- After presenting, leave articles as **unread** unless the user says otherwise.
- When zero new articles: just say "no new articles" or equivalent.

## Adding a New Blog

1. Try: `blogwatcher add "<name>" "<url>"` — auto-discovers RSS/Atom feed.
2. If no feed found, look for RSS links in the page source or try common paths (`/feed`, `/rss`, `/atom.xml`, `/feed.xml`).
3. If still no feed, use HTML scraping: `blogwatcher add "<name>" "<url>" --scrape-selector "<css>"` — find a CSS selector matching article links on the blog's main page.
4. Verify: `blogwatcher scan` then `blogwatcher articles --blog "<name>"`.
5. Add an `[interest.blogs."<name>"]` entry to `~/.blogwatcher/config.toml` with a tailored interest prompt.

## Notes

- State is stored locally in `~/.blogwatcher/`.
- Config at `~/.blogwatcher/config.toml` — `[summary]` and `[interest]` sections with per-blog overrides.
- HTML scrape blogs need a CSS selector — may break if site redesigns. RSS blogs are more reliable.
- Blogs with a scrape selector skip RSS feed discovery during scan (avoids slow probes). Use `--feed-discovery` to override.
- `scan` is idempotent — safe to run multiple times.
- `summary` and `interest` are idempotent but cost money on first run — avoid `--refresh` unless needed.
- No built-in scheduling — run manually or set up a cron job.
- Use `blogwatcher export` to back up blog definitions as a portable shell script.

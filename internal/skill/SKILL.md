---
name: blogwatcher
description: Monitor blogs, websites and RSS/Atom feeds for new articles, summarize them with AI, and classify interest — using the blogwatcher CLI.
---

# blogwatcher — Blog & Feed Monitor

Binary: `blogwatcher` (Go CLI, expected in PATH)
Config: `~/.blogwatcher/config.toml`
Database: `~/.blogwatcher/blogwatcher.db` (SQLite, created and managed by the CLI)

## Start Here: Scope and Safety

If behavior differs from this document, check `blogwatcher --version` and `COMMAND --help`.

- Typical run: follow [Standard Workflow](#standard-workflow) (scan → interest → clean up → review → present). For a cached review only (no network or LLM calls), use its step 0.
- `scan` performs network requests and writes discoveries, RSS summaries, and scan state. Repeatability is **not** read-only safety. Run only within an authorized scan/update task.
- `summary`/`interest` can fetch, generate, incur model costs, and write caches even without `--refresh` (including short-RSS upgrades or configured HN enrichment). `--json`, `--extractive`, and a cache hit are not read-only guarantees. Use `articles --json` to inspect cached summaries/classifications/HN data. There is no cache-only or dry-run flag.
- `read`, `unread`, add/remove/rename, and config changes require user authorization. Do not automatically mark `hide` articles read unless explicitly approved or covered by a standing instruction. `-y` skips a CLI prompt; it does not grant permission.
- For agents, use `--json` on blogs/articles/summary/interest. Scan/read/export have no documented JSON flag.

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
| `blogwatcher articles --filter norm` | Show normal-classified plus unclassified articles |
| `blogwatcher articles --filter pref` | Show only `prefer`-classified articles |
| `blogwatcher summary --filter prefer` | Summarize only prefer-classified articles |
| `blogwatcher interest --filter normal` | Classify normal-classified plus unclassified articles |
| `blogwatcher articles --since 7` | Show posts from the last 7 days |
| `blogwatcher articles --sort oldest` | Order by date (earliest first; default is `newest`) |
| `blogwatcher articles -s` | Show cached summary text alongside articles |
| `blogwatcher articles -v` | Show blog, engine, summary size, timestamp, and cached HN metadata |
| `blogwatcher read <id> [id...]` | Mark article(s) as read |
| `blogwatcher read --filter hide` | Mark all hide-classified unread articles as read |
| `blogwatcher read --filter all` | Mark all unread articles as read |
| `blogwatcher unread <id>` | Mark article back to unread |
| `blogwatcher summary [id]` | Summarize article(s) (AI or extractive fallback) |
| `blogwatcher summary --all` | Summarize all articles including read |
| `blogwatcher summary --refresh` | Re-generate even if cached |
| `blogwatcher summary --extractive` | Force non-LLM snippet mode |
| `blogwatcher summary --sort oldest` | Order output by date (earliest first; default `newest`) |
| `blogwatcher interest [id]` | Classify article(s) as prefer/normal/hide |
| `blogwatcher interest --all` | Classify all articles including read |
| `blogwatcher interest --refresh` | Re-classify even if cached |
| `blogwatcher interest --refresh-summary` | Re-generate summaries before classification |
| `blogwatcher interest --extractive` | Use extractive fallback for missing summaries; classification can still call the LLM |
| `blogwatcher interest -s` | Show cached summary text alongside interest results |
| `blogwatcher interest -v` | Show classifier, judged time, summarizer, and HN metadata |
| `blogwatcher interest --sort oldest` | Order output by date (earliest first; default `newest`) |
| `blogwatcher add <name> <url>` | Add blog; feed discovery happens during scan unless a feed URL is supplied |
| `blogwatcher add <name> <url> --feed-url <rss>` | Add blog with explicit feed URL |
| `blogwatcher add <name> <url> --scrape-selector <css>` | Add blog with HTML scraping |
| `blogwatcher remove <name>` | Remove blog from tracking (destructive; confirmation can be skipped with `-y`) |
| `blogwatcher rename <old-name> <new-name>` | Rename a tracked blog |
| `blogwatcher export` | Export blog definitions as portable shell script |
| `blogwatcher skill` | Print this skill document |

Common flags for `articles`, `summary`, `interest`, and bulk `read`: `--filter <value>` where values are `all`, `hide`, `normal`/`norm`, `prefer`/`pref`; repeat or comma-separate values to combine them.
Common flags for `articles`, `summary`, and `interest`: `--since <YYYY-MM-DD|days>` narrows bulk/list queries on or after the cutoff, using published date and falling back to discovered date. Days must be non-negative. `--since` cannot be combined with explicit IDs.
`articles` explicit IDs include read articles, ignore `--blog`/interest filters, and are date-sorted rather than kept in argument order; repeated IDs remain repeated. Do not treat this mode as an unread-only list.
`articles` has **no `--limit`**: bound list queries with `--since`/`--blog`, or fetch selected IDs.
Common flags for `summary` and `interest`: `--blog <name>`, `--limit N`, `--workers N`, `--model <model>`, `--verbose`, `--hn`, `--hn-refresh`, `--hn-limit N`
Common flags for `scan`, `summary`, and `interest`: `--debug` (timestamped profiling output on stderr)
Common flags for `blogs`, `articles`, `summary`, and `interest`: `--json` (see [JSON Output](#json-output))
Scan-specific: `--feed-discovery` (try RSS/Atom discovery even for blogs with a scrape selector), `--silent`/`-s` (only `scan done` on completion), `--workers`/`-w` (default 8). Note `-s` means something different on articles/interest.
Help defaults: summary/interest `--limit 80`, `--workers 8`, `--hn-limit 40`; date sort defaults to `newest`. `read --blog` and `--yes` apply to filter-based bulk marking, not ID selection.

## Summary Pipeline

`summary` fetches the article page and stores a short summary. Engine values (`summary_engine`):
- `openai`: LLM summary (default when an API key is set).
- `snippet`: first ~2000 characters (no API key, `--extractive`, or LLM failure).
- `verbatim`: article under 250 words, stored as is.
- `rss`: feed description stored during `scan`.
- `+jina` suffix (e.g. `openai+jina`): the page text came from Jina Reader (see `jina_fetch`).

- **Caching:** summaries are reused unless `--refresh` is used. Exception: RSS summaries under 500 characters are upgraded on the next `summary`/`interest` run. If the upgrade fails (e.g. HTTP 403), the RSS summary is kept.
- **Notes:** with exit code 0, a note such as `Summarization failed … Kept existing RSS summary` is a fallback hint, not a hard error or `AUTOMATION_FAILED`. Continue with the kept summary; report real command failures separately.
- **Cost control:** `--limit N` (default 80) caps article summaries. `--workers N` sets concurrency. See [Hacker News Enrichment](#hacker-news-enrichment) for `--hn-limit`.

### Summary Configuration

Top-level keys must come before the first `[section]` in TOML.

```toml
user_agent = "..."               # optional site User-Agent; default blogwatcher/<version> (+repo URL)
jina_api_key = "jina_..."        # or JINA_API_KEY env (env wins)
jina_fetch = "fallback"          # none | primary | fallback; default: primary with a key, else none

[summary]
openai_api_key = "sk-..."        # or OPENAI_API_KEY env
model = "gpt-6-luna"
system_prompt = "..."
max_request_bytes = 40960        # max article text sent to LLM
hackernews = false               # enable --hn behavior without passing --hn
hackernews_prompt = "..."
hackernews_max_request_bytes = 204800
```

## Interest Classification Pipeline

The `interest` command classifies each article as **prefer**, **normal**, or **hide** based on its summary.

**Flow:** ensure summary exists → build prompt with blog name + classification policy + summary → LLM returns JSON `{"state": "...", "reason": "..."}`.

- **Dependency:** interest classification always requires a summary. If missing, `interest` auto-generates one first.
- **Input limit:** `[interest].max_request_bytes` defaults to 12288 and caps the cached summary text sent to the classifier.
- **Labels:**
  - `prefer` — high signal, surface to user
  - `normal` — worth noting, not urgent
  - `hide` — low signal; omit from a digest if appropriate, but mark read only with authorization
- **Per-blog prompts:** each blog can have its own `interest_prompt` in config. Falls back to the global prompt. If no prompt exists for a blog, articles are left unclassified.
- **Caching:** classification is stored in the database with a timestamp. Re-run with `--refresh` to reclassify.
- **Output:** JSON parsed from LLM response. Invalid JSON or states are rejected and the article is left unclassified.

### Interest Configuration

```toml
[interest]
openai_api_key = "sk-..."
model = "gpt-6-luna"
system_prompt = "..."                # classifier system prompt (controls JSON output format)
interest_prompt = "Prefer ..."       # global classification policy

[interest.blogs."simonwillison"]
interest_prompt = "Prefer LLM tooling and Datasette posts; hide generic link roundups."

[interest.blogs."macrumors"]
interest_prompt = "Prefer Apple hardware releases; hide accessory reviews and rumors."
```

The `interest_prompt` is the user-facing classification policy — what matters to you. The `system_prompt` controls the LLM's output format and should rarely need changing.

## Hacker News Enrichment

Adds the matching HN discussion (points, comments, summary) to each article via Algolia HN search by URL. Works with `summary`, `interest`, and (display only) `articles -v`.

**Cached inspection needs no fetch.** `articles --json` includes cached `hn`. `articles -v` / `interest -v` show HN metadata; add `-s` for HN summary text; `summary -v` shows HN summary text. Do not run `summary`/`interest` only to inspect cached HN data.

**Flags gate fetching/summarizing only:**
- `--hn` (or `[summary].hackernews = true`) enables fetching missing HN data and generating missing HN discussion summaries for selected articles. Available on `summary` and `interest`.
- `--hn-refresh` re-fetches HN metadata and regenerates HN summaries even if cached.
- `--hn-limit N` (default 40) caps how many *new* HN discussion summaries are generated per run. Cached results and HN metadata lookups are not counted. Hide-classified articles receive HN metadata but no HN summary in `interest`; the classifier does not use HN data.

## JSON Output

`blogs`, `articles`, `summary`, and `interest` support `--json`: one JSON document on stdout, with no human text.

**Envelopes:**
- `blogs --json` → `{"blogs": [ { id, name, url, feed_url?, scrape_selector?, last_scanned?, stats: {total, unread, hide, normal, prefer} } ]}`
- `articles --json` → `{"articles": [ <article> ]}`
- `summary --json` → `{"summaries": [ { article: <article>, blog_name?, engine?, cached?, upgraded?, warning?, hn? } ]}`
- `interest --json` → `{"interests": [ { article: <article>, blog_name?, engine?, cached?, skipped?, note?, hn? } ]}`

**`<article>` fields:** `id`, `blog_id`, `blog_name?`, `title`, `url`, `published_date?`, `discovered_date?`, `is_read`, `summary?`, `summary_engine?`, `interest_state?`, `interest_reason?`, `interest_engine?`, `interest_judged?`, `hn?`.

**Types:** optional fields are omitted, never `null`. An empty list is `{"articles": []}`. `stats.total` includes read articles; `stats.hide/normal/prefer` count only unread ones. `blogs --unread` keeps blogs with `stats.unread > 0`.

**`hn` object** (only when HN-checked): `fetched`, `found`; when found also `item_id`, `url`, and optional `points`, `comments`, `summary`, `warning`, `cached`. Do not expect `points`/`comments` to be present (0 is omitted).

**Errors:** most failures emit `{"error": "..."}` on stdout with exit 1. Flag-parse errors (e.g. an unknown flag) print plain text on stderr. Check the exit status before parsing.

**Tip:** JSON output is the same with or without `-v`/`-s`; those flags only change the human output.

## Standard Workflow

When doing a run, typical does 1-4 + presentation, occasionally 0.

### 0. (OPTIONAL) Inspect cached state

Use `blogwatcher blogs --unread --json` and choose a bounded list query:

```sh
blogwatcher articles --since 7 --filter norm --json # normal + unclassified
blogwatcher articles --since 7 --filter pref --json # prefer only
blogwatcher articles --since 7 -f prefer,norm --json
blogwatcher articles --blog "BLOG_NAME" --json
```

Hints: List queries default to unread; use `--all` for read history. `articles --json` already includes available cached summaries and HN data. Copy IDs/blog names from current output, not illustrative examples. Completion: identify selected article IDs and whether cached data suffices.

### 1. Scan for new articles

```
blogwatcher scan
```

Wait for completion. This fetches new articles from all tracked blogs via RSS/Atom or HTML scraping. RSS feed descriptions are automatically stored as initial summaries.

### 2. Classify interest (auto-summarizes)

```
blogwatcher interest -v
```

Wait for completion. This also generates summaries for articles that lack one, then classifies all unread articles. The `-v` flag shows engine and cache metadata.

### 3. Clean up

Worth to do before reviewing/reading, as it will mark as 'read' articles not worth checking ('hide' status).

```
blogwatcher read --filter hide -y    # mark all hide-classified as read
```

### 4. Review articles

Choose one of the examples, or mix them. All show only *unread* ones.

```
blogwatcher articles -f norm -v      # show only norm(al) articles
blogwatcher articles -f pref -v      # show only pref(er) articles
blogwatcher articles -f prefer,norm  # prefer plus normal ones
blogwatcher articles --since 7 -v    # time-bounded review for last 7 days
blogwatcher articles -v -s           # all unread articles with summaries
blogwatcher articles -v -s 42 ART_ID # specific article w/ ART_ID with summary
blogwatcher interest -v -s           # interest results with summary text of all unread articles
```

**IMPORTANT:** Always copy URLs exactly from `blogwatcher articles` output. Never reconstruct or guess URLs.

## Presenting Articles to Users

Group articles by blog. Use interest labels and summaries for context.

For cached HN data, see [Hacker News Enrichment](#hacker-news-enrichment). If the user asks for community reaction, controversy, external validation, or Hacker News context for articles that haven't been enriched yet, use a selected-ID `blogwatcher summary ID --hn --hn-limit 1 --json` within the authorized enrichment task. It may also generate/upgrade an article summary. Do not use `--hn-refresh` unless the user asks for fresh HN data or the cached HN data is clearly stale.

```
📰 Blogwatcher — N new articles

⭐ **blogname** (2 prefer, 1 normal):
• [prefer] Article Title — https://example.com/...
  Two-sentence summary from cached data.
  HN, if shown: points/comments plus a brief discussion summary.
• [normal] Another Article — https://example.com/...
  Summary here.

**otherblog** (1 prefer):
• [prefer] Some Post — https://other.com/...

remaining-blogs — no updates.
```

- Include published date and interest reason when relevant.
- After presenting, leave articles as **unread** unless the user says otherwise.
- Distinguish “no unread articles” (cached listing) from “no new articles” (completed scan found none). Do not claim current feed freshness from cached state.

## Tracked Blogs

Run `blogwatcher blogs --json` for list of blogs, or `blogwatcher blogs -u --json` to show only blogs with unread articles.

## Adding a New Blog

Only for an authorized add/config task:

1. Determine whether an explicit RSS/Atom URL or HTML link selector is needed. Look for feed links or common paths (`/feed`, `/rss`, `/atom.xml`, `/feed.xml`) if appropriate; prefer RSS when available.
2. Register once: `blogwatcher add "<name>" "<url>"`, optionally with `--feed-url "<rss>"` or `--scrape-selector "<css>"`. Without a feed URL, discovery happens during scan (unless a scrape selector suppresses it). Do not blindly re-add an existing blog; there is no update command.
3. Verify the definition with `blogwatcher blogs --json`.
4. Within the authorized task, scan only the added blog: `blogwatcher scan "<name>"`, then `blogwatcher articles --blog "<name>" --json`.
5. If classification setup is requested, inspect and merge an `[interest.blogs."<name>"]` entry into `~/.blogwatcher/config.toml` with a tailored policy; preserve existing config and credentials.

## Notes

- State is stored locally in `~/.blogwatcher/`.
- Config at `~/.blogwatcher/config.toml` — top-level `user_agent`, `jina_api_key`, `jina_fetch`, plus `[summary]` and `[interest]` sections with per-blog overrides.
- HTML scrape blogs need a CSS selector — may break if site redesigns. RSS blogs are more reliable.
- Blogs with a scrape selector skip RSS feed discovery during scan (avoids slow probes). Use `--feed-discovery` to override.
- Repeated `scan` still performs network I/O and writes scan state.
- `summary`/`interest` are cache-aware, not read-only; avoid `--refresh`/`--refresh-summary`/`--hn-refresh` unless needed and authorized.
- No built-in scheduling — run manually or set up a cron job.
- Use `blogwatcher export` to back up blog definitions as a portable shell script.

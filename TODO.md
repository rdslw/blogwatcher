# TODO (ideas and tasks)

## Port upstream SQLite per-blog User-Agent override (added: 2026-09-06)

Priority: low; probably low-value for now while our global User-Agent setting is sufficient. Revisit when individual blogs need different identities.

Upstream: [Hyaxia/blogwatcher PR #7](https://github.com/Hyaxia/blogwatcher/pull/7), merged May 5, 2026. Adds `add --user-agent`, persists `blogs.user_agent` in SQLite, and uses it for feed discovery, fetching, and scraping. Key commits: [per-blog overrides](https://github.com/Hyaxia/blogwatcher/commit/9f5aa8a07d5b6b24e8fe7452fb8b414d447aa70f), [removal of the global fallback](https://github.com/Hyaxia/blogwatcher/commit/211ccafe7be5f9c39c5f0abacb0809586f9c4843), and [migration helper](https://github.com/Hyaxia/blogwatcher/commit/47e415dc5d7a4181baaeb18f7abafeaf612851ec).

Proposed adaptation:

- Preserve precedence: nonblank per-blog override → global `config.toml` setting → versioned BlogWatcher default.
- Store only explicit overrides in SQLite; support adding, updating, and clearing them without recreating blogs.
- Apply consistently to discovery, feed retries, scraping, and article fetching for summary/interest workflows through our shared HTTP helper.
- Include overrides in verbose blog output, JSON, and exported definitions; test inheritance and mixed-blog concurrent requests.
- Port selectively, retaining our migration behavior and upstream provenance; include `(upstream)` in import commit headlines.

## Reevaluate migration helper (added: 2026-09-06)

Low priority; reevaluate, not a commitment to adopt. If migration complexity or startup overhead becomes a problem, compare our current approach with [upstream's schema-inspection helper](https://github.com/Hyaxia/blogwatcher/commit/47e415dc5d7a4181baaeb18f7abafeaf612851ec). Any replacement should preserve column defaults and tolerate concurrent processes adding the same column.

## Blog Health / Structure Checks (added: 2026-03-26)

Goal: detect blogs that may have become unhealthy, stale, or structurally incompatible with current tracking settings.

Ideas:
- Add a command or periodic check to verify tracked blogs are still healthy.
- If `scrape_selector` is set, test whether it still matches usable article links and warn if the blog structure appears to have changed.
- Detect stale blogs with no new posts for a configurable threshold, default idea: 365 days.
- Make the stale threshold configurable in `config.toml`.
- Check that the blog URL being used (main page and/or RSS URL) still resolves successfully.
- Verify the URL fetch returns HTTP 200.
- Verify the fetched response body length is greater than 1 KB.

Open questions:
- Whether this should be a dedicated command like `blogwatcher health` or part of `scan`.
- Whether stale detection should use newest discovered article, newest published article, or last successful scan.
- Whether the URL check should validate both main page and RSS URL when both exist.
- How warnings should be surfaced in CLI output and whether to persist health-check results.

# TODO (ideas and tasks)

## make build/release simpler and/or connected

Goal: to not have conflicted binaries 

sometimes I use 'make build', sometimes 'make release', those two shall know about other and invalidate.
e.g. 'make release' shall either remove normal binary or symlink it to release
'make build' shall remove release

or differently, you may propose it.


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

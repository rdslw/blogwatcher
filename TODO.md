# TODO (ideas and tasks)

## Re-evaluate --debug output for summary and interest phases (added: 2026-04-22)

Goal: once interest prompts are configured and summary/interest run with real LLM work, re-run
`blogwatcher summary --debug` and `blogwatcher interest --debug` and evaluate whether the debug
output provides enough signal for performance analysis.

Known issues from initial analysis (2026-04-22, all articles were skipped — no interest prompts configured):
- Per-article start/done pairs for skipped or cached articles are pure noise (~1µs each, 324 lines for 162 articles saying nothing useful). Consider suppressing individual lines for trivially-skipped articles and emitting a single summary line instead (e.g. "skipped 162 articles (no interest prompt)").
- Worker distribution is pathological when all work is trivial — one worker drains the entire channel before others wake up. Not a bug (no real work is lost), but the debug log misleadingly suggests only 2–3 workers ran.
- The scan phase debug output is already good — tested with real network I/O and clearly reveals bottlenecks.

Re-analysis checklist:
- Run with interest prompts configured so articles actually hit the LLM.
- Verify that per-article LLM call timings are visible and useful.
- Decide whether skipped/cached articles should be logged individually or summarized.
- Check if worker distribution looks reasonable under real load.
- Ask the user how to proceed (no implemenation without approval)


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

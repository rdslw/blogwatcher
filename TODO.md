# TODO (ideas and tasks)

## Update SKILL.md docs from elfka source (added: 2026-03-26)

Goal: document the current summary flow, interest classification flow, and their end-to-end interaction in `SKILL.md`, using the elfka source as the reference input.

Tasks:
- Update `SKILL.md` with the summary pipeline: trigger points, inputs, outputs, caching, and configuration.
- Document the interest classification pipeline: dependency on summaries, prompts, labels, refresh behavior, and blog-specific overrides.
- Describe the full user flow connecting scan, summary generation, interest classification, and article review.
- Cross-check the documentation against the elfka source before considering the task complete.

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

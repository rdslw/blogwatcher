# TODO (ideas and tasks)

## Migrate module path to fork (added: 2026-03-25)

Goal: rename the Go module from `github.com/Hyaxia/blogwatcher` to the fork-owned path.

When doing this, update all affected places together:
- `go.mod` module path
- Go imports throughout the repo
- linker flag targets in build tooling
- release configuration such as `.goreleaser.yaml`
- docs and any install/build instructions that reference the old module path

## Interest Classifier with LLM (added: 2026-03-24)

Goal: add a separate LLM-based "interest" classification step for articles, distinct from summarization.

High-level shape:
- Run a dedicated LLM call for interest classification.
- Use the article summary as the classifier input.
- Persist the classification result in the database, similar to stored summaries.
- Support three states: `prefer`, `normal`, `hide`.
- Show the interest state as a tag next to each article in CLI output.

Proposed user-facing flow:
- Add a separate command, likely one of:
  - `judge`
  - `interest`
  - `rank`
  - `triage`
- Revisit naming later; `interest` is the clearest working name for now.
- Command should work for a single article or in batch, similarly to `summary`.

Config direction:
- Add per-blog interest-classification rules to config.
- Rules should be configurable either as:
  - a custom prompt, or
  - a structured checklist / rubric
- Keep this per blog, so different blogs can define different notions of "interesting".

Persistence direction:
- Add article-level DB fields for:
  - interest state
  - optional explanation / reason
  - optional timestamp for when interest was last judged
- Treat this like summary caching: once judged, reuse until refreshed.

CLI / UX ideas:
- Show the interest state as a tag in `articles` output.
- Consider also showing it in `summary`.
- Later, allow filtering or prioritization by interest state:
  - show preferred first
  - hide `hide` items by default, or make that optional

Open design questions for later:
- Exact command name
- Exact config shape for per-blog prompts/checklists
- Whether to require an existing summary, or auto-generate one first if missing
- Whether classifier output should include only the label, or also a short explanation
- Whether `hide` should affect only display, or also default summary batching

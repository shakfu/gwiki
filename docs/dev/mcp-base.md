# Base hashes on destructive MCP tools

Any MCP tool that deletes a page or replaces its body must take a `base`: the
hash from `gwiki_read`. Decided 2026-09-28, closing W8 in `REVIEW.md`.

## Rule

- A tool that removes a page, or replaces text it did not name exactly, takes
  `base` and refuses on a mismatch, as `gwiki_write` does.
- Pass the base through to `commit`. Do not let the `internal/wiki` function
  read the page and use its own hash.

## Why

An agent reads a page, reasons, then acts. The page can change in between.
Two hazards differ here:

- **Lost update**: two writers at once. `SetBody`, `Remove`, `Tag`,
  `SetTaskStatus` and `Fix` already refuse this. Each checks the hash it read
  itself, and `commit` checks again before each rename.
- **Stale decision**: acting on a version seen earlier. Only the hash of that
  version detects it, so the caller must hold it and pass it.

`SetBody` and `Remove` take no base today. Their only callers are
`gwiki edit -m`/`--stdin` and `gwiki rm`. Neither reads the page first, and no
CLI command prints a hash, so a base would have no source. An MCP tool has one:
the agent's `gwiki_read`.

## Current tools

- `gwiki_write` and `gwiki_edit` take `base`.
- `gwiki_fix_link` takes none. `Fix` checks the link text at its span, so a
  changed link is refused.
- `gwiki_set_task` takes none. A checklist item is checked by its text; a task
  page changes only its front matter `status`.
- There is no delete tool and no body-replace tool.

# TODO

IDs refer to `REVIEW.md` (commit `2cdc3d9`, 2026-09-27), which holds the evidence and reproduction steps.

## Low

- [ ] P4. Indirect dependencies lag: `golang.org/x/text` v0.3.8 (v0.44.0 is out), `go-runewidth` v0.0.16 (v0.0.31).

- [ ] Check the mouse and the clipboard by hand in Terminal.app, iTerm2, tmux and over SSH: click, link double click, wheel, drag and release, `yy`, then cmd-v elsewhere. The tests drive recorded zones and stand-in clipboard tools, not a terminal. Not in `REVIEW.md`.

- [ ] "copied" is shown for an OSC 52 copy that the terminal may drop: over SSH, on Windows, and where no clipboard tool is installed. gwiki cannot tell. Windows could use PowerShell's `Set-Clipboard`, which keeps non-ASCII text, unlike `clip.exe`. Not in `REVIEW.md`. `tui/clipboard.go`

- [ ] The editor draws a tab as `display.TabWidth` spaces wherever it is; the reader advances to the next tab stop, as vim does. Matching them changes `vim.Wrap`, `columns` and the TUI's `colAt`. Not in `REVIEW.md`.

## Optimize

- [ ] Highlighting while typing in a code block. Each edit inside a fenced block re-lexes the whole block: about 1.2 ms per KiB, so 19 ms per keystroke at 16 KiB and 36 ms near the 32 KiB cap (measured on an M-series Mac). An unclosed fence makes the rest of the page one block, at the same cost, until it is closed. Edits outside a block cost under 0.5 ms, since other blocks come from the cache. On a machine that cannot lex the block within 50 ms, it is drawn plain and keystrokes alternate between a 50 ms wait and an immediate warning. Fix: lex only after a pause in typing, about 150 ms; until then draw the lines outside the edited block with their last spans, and the block plain or with its old spans where its lines are unchanged. Not in `REVIEW.md`. `tui/wiki_edit_view.go` (`codeSpans`)

- [ ] T24. `Refresh` and the draft write run inside `Update` each second, so a slow disk stalls input. `tui/wiki.go` (`pollDisk`)

## Design

- [ ] D1. Move validation into `internal/wiki`: `Task` carries its text and is re-checked, mutators take a base hash, one symlink-safe open, link schemes classified once. The individual fixes (W4, W5, W8, S5) are done; this is the structural cleanup that keeps them from regressing.

- [ ] D2. `WikiModel` shares `cursor`, `scroll` and `input` across screens; an invariant is kept by hand. `internal/tui/wiki.go:61-144`

- [ ] D4. Two aligned-table types: `cli/format.go` (plain columns for pipes) and `tui/table.go` (shrinking, styled columns). Merge only if one can serve both. The rest of `REVIEW.md` D4 is done.

## Ideas

- [ ] `-C <dir>`, as in `git -C`: open the project at a path instead of discovering one. Useful for scripts and per-project MCP registration.

- [ ] A view across projects: open tasks from every known project. It needs a list of known projects, which nothing records today.

- [ ] Show an unlabelled wiki link to a heading as `Drawing > Markdown`, as Obsidian does, not as written (`Drawing#Markdown`). The default label is set in the parser, so the terminal and browser views change together. `internal/markdown/wikilink.go:85-87`

- [ ] cmd-c to copy gwiki's selection. The terminal takes cmd-c, and Bubble Tea v1 cannot see Cmd. Bubble Tea v2 reports it under the kitty keyboard protocol, in terminals that support it and do not bind cmd-c themselves. A large migration for one key; copy on select covers the case now.

- [ ] A backlink in the browser view scrolls to the link itself and highlights it. It now lands on the heading above, since the rendered page has no line numbers. Needs each link element marked with its source line (`data-line`), in the renderer, the page API and `app.js`. `internal/markdown/html.go`, `internal/webwiki/api.go` (`backHref`)

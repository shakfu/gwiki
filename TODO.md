# TODO

IDs refer to `REVIEW.md` (commit `2cdc3d9`, 2026-09-27), which holds the evidence and reproduction steps.

## Critical

## High

- [x] W1. `Create` loops forever when `os.Stat` fails with anything but "not exist" (`ENAMETOOLONG`, `ENOTDIR`). `internal/wiki/ops.go:94-99`

- [x] W2. A move that fails part-way leaves some pages rewritten and the cache stale. Write temp files first, rename `to`, then linking pages, then remove `from`. `internal/wiki/write.go:103-113`, `internal/wiki/move.go:187-197`

- [x] W3. `commit` returns the `Refresh` error after the write has landed; one unreadable page fails every write and `Open`. `internal/wiki/write.go:114-116`

- [x] S1. Symlinks escape the wiki and the repository: read, write and create through MCP and the web view. Consider `os.Root`. `internal/wiki/write.go:154-172`, `internal/wiki/refresh.go:231`

- [x] T1. A buffer with unsaved changes cannot be saved after its page is deleted; `:w` and `:w!` both conflict. `internal/tui/wiki_edit.go:141-156`

- [x] T2. An operator followed by `/` or `?` runs at once on the cursor character and stays pending; register is ignored. `internal/vim/motion.go:169-173`

- [x] T3. Unbounded counts crash (OOM, overflow panic) or hang the process, losing unsaved edits. `internal/vim/normal.go:65-75`, `motion.go:292,330,415,622`

## Medium

- [x] P1. goldmark v1.7.16 has a reachable XSS (GO-2026-5320); update to v1.7.17 or later. `go.mod`

- [x] W4. `Promote` slices before it checks bounds; a stale `Task` panics. `internal/wiki/ops.go:443,449`

- [x] W5. `SetTaskStatus` trusts `Task.Box` alone; a stale task ticks the wrong item. `internal/wiki/ops.go:420-428`

- [x] W6. A move does not rewrite a page's wiki links to itself; `--dry-run` does not report it. `internal/wiki/move.go:110,154`

- [x] W7. A move of an untitled page breaks `[[title]]` links. `internal/wiki/move.go:85-86`

- [x] W8. `SetBody`, `Remove`, `Tag`, `SetTaskStatus` (task page) and `Fix` take no base hash; the README now says so. Closed as documented: no caller holds an earlier read to pass as a base. Any future MCP delete or body-replace tool must take a `base`; see `docs/dev/mcp-base.md`. `internal/wiki/ops.go:235-325,402-410`, `internal/wiki/fix.go:202-215`

- [x] W9. `Open` deletes the cache on any connect error, including lock timeout. `internal/wiki/cache.go:113-118`

- [x] W10. The write check does not cover writers outside gwiki; re-check before each rename. `internal/wiki/write.go:88-112`

- [x] W11. Change detection uses size and mtime only; `rsync -t`/`tar` edits are never re-indexed. `internal/wiki/refresh.go:86`

- [x] S2. The browser view matches checkboxes to tasks by index; a blockquoted task shifts them. `internal/webwiki/assets/app.js:286-291`

- [x] S3. The language server keys buffers by the client's URI string; unsaved-changes guard on rename misses. `internal/lsp/server.go:350`, `features.go:146,610`

- [x] S4. `javascript:`, `data:`, `vbscript:` targets reach the page via autolinks and `href`. Allowlist `http`, `https`, `mailto`. `internal/webwiki/api.go:221-222`, `app.js:322`

- [x] S5. The web view and MCP apply different rules to the same write (line endings, task text check, empty base). `internal/webwiki/api.go`, `internal/mcp/wiki.go`

- [x] S6. `documentSymbol` returns line -1 and an empty name for an empty heading. `internal/lsp/features.go:287-290`

- [x] T4. `ctrl-c` and `q` quit with unsaved changes. `internal/tui/wiki.go:542-545`, `internal/tui/keys.go:73`

- [x] T5. `gwiki edit` discards the edited text on a conflict. `internal/cli/wiki.go:704-718`, `internal/cli/commands.go:126,135`

- [x] T6. `ctrl-o` in insert mode breaks undo. `internal/vim/editor.go:296-301`

- [x] T7. Completion bypasses the undo log and the draft hook. `internal/tui/wiki_edit.go:336-344`

- [x] T8. The CLI prints escape sequences from page content in `links`, `show`, `check`, `tasks`. `internal/cli/format.go:85-88`

- [x] T9. The preview prints a raw `due` value. `internal/tui/wiki_view.go:322-323`, `wiki_lists.go:199-212`

- [x] T10. An exclusive motion with an empty range deletes one character (`d0`, `dh`, `db` at column 0). `internal/vim/motion.go:201-214`

- [x] T11. `.` records keys that changed nothing; visual-mode operators repeat as nothing. `internal/vim/normal.go:38-41,105-107,226-231`

- [x] T12. `c` takes two undo steps. `internal/vim/normal.go:424-440`

- [x] T13. A pasted `\r` is saved literally; a one-rune paste is dropped. `internal/tui/wiki_edit.go:353,421-427`

- [x] T14. The buffer wraps by rune count, not by column (CJK, tabs). `internal/vim/motion.go:586-607`

## Low

- [x] P2. No CI. Add one running `make check`, `staticcheck`, `govulncheck`. `Makefile:60`

- [x] P3. Unused symbols: `cursorBefore`, `cursorAfter`, `(*Buffer).end`, `lastReg`. `internal/vim/buffer.go:46,100`, `editor.go:98`

- [ ] P4. Indirect dependencies lag: `golang.org/x/text` v0.3.8, `go-runewidth` v0.0.16.

- [ ] P5. Docs describe behaviour the code lacks: query-refresh claim (`internal/wiki/project.go:1-6`). (`gwiki edit` now keeps the text on a conflict, so its comment holds.)

- [ ] W12. No `fsync` of the parent directory after rename or remove. `write.go:105,149`

- [ ] W13. A moved page gets mode 0644; the empty source directory stays. `write.go:126-129`, `move.go:193`

- [x] W14. A page deleted between scan and parse fails the whole refresh. `refresh.go:100-104`

- [ ] W15. Rename detection matches on content hash alone; `renames` is never pruned. `refresh.go:151`

- [ ] W16. `git` runs without `core.quotePath=false`; non-ASCII pages get no author or date. `overview.go:110,133`

- [ ] W17. The tokenizer splits on combining marks; FTS5 does not. NFD queries find nothing. `search/search.go:78`

- [ ] W18. `FindTask` treats any `text:digits` as `page:line`. `ops.go:331-347`

- [ ] W19. `_txlock=immediate` makes read-only transactions take the write lock. `cache.go:155`, `ops.go:490`

- [ ] W20. A directory link indexed before its `README.md` exists stays `kind=file`. `nested.go:45-63`

- [ ] S7. `"id": null` is answered as a request; `jsonrpc` is not checked. `lsp/server.go:169`

- [ ] S8. A second `initialize` succeeds and leaks the first cache handle. `lsp/server.go:291`

- [ ] S9. No panic recovery in LSP or MCP. `lsp/server.go:192`, `mcp/mcp.go:192`

- [ ] S10. Web server sets only `ReadHeaderTimeout`; no `IdleTimeout`, no graceful shutdown. `webwiki/server.go:205`

- [x] S11. `/api/file` serves any repository file under 1 MB, including `.env`, `.git/config`, `.gwiki/cache.db`. `webwiki/api.go:289-317`

- [ ] S12. Web errors return absolute paths. `webwiki/api.go:303`

- [x] S13. `checkDisk` drops `Refresh` errors; live updates stop silently. `webwiki/server.go:226-233`

- [ ] S14. MCP has no frame size limit. `mcp/mcp.go:144-159`

- [ ] S15. A malformed `%` escape in the hash throws outside the `try`. `webwiki/assets/app.js:161-169`

- [ ] S16. Comment says raw HTML is escaped; it is omitted. `markdown/html.go:32`

- [ ] T15. `:s` replaces on the matched substring, so `\B` and similar assertions fail. `vim/ex.go:295`

- [ ] T16. A `\n` replacement shifts lines; the `:s` range does not follow. `vim/ex.go:283-303`

- [ ] T17. Only `/` separates `:s` parts. `vim/ex.go:129`

- [ ] T18. `r<tab>` writes `t`; `f<tab>` searches for `t`; `3r<enter>` writes 3 newlines. `vim/normal.go:561-568`

- [x] T19. On `- [x] done [ ] other`, the toggle changes the second box. `vim/editor.go:479`

- [x] T20. The draft write is not atomic. (Its errors are now reported and retried.) `tui/wiki_edit.go:132-136`

- [x] T21. A restored stale draft takes the current hash, so `:w` overwrites the newer page. `tui/wiki_edit.go:108-109`

- [ ] T22. `gwiki tag other --json` adds the tag `--json`. `cli/wiki.go:862-875`

- [ ] T23. `orphans` and `tasks` ignore extra arguments; `search -n -5` is accepted. `cli/wiki.go:555-560`

- [ ] T24. `Refresh` and the draft write run inside `Update` each second. `tui/wiki.go:492-494`

- [ ] T25. Bidirectional overrides (U+202E) and zero-width characters pass the display filter. `display/display.go:30`

- [x] T26. A paste in normal mode ran as keystrokes, so pasted text ran as commands. Found 2026-09-28, not in `REVIEW.md`. `internal/tui/wiki_edit.go` (`keyContent`)

- [x] T27. `dj` on the last line deletes it; vim fails and does nothing. `k` on the first line likewise (inferred). Found 2026-09-28, not in `REVIEW.md`. `internal/vim/motion.go` (`opRange`)

## Design

- [ ] D1. Move validation into `internal/wiki`: `Task` carries its text and is re-checked, mutators take a base hash, one symlink-safe open, link schemes classified once. Closes W4, W5, W8, S5 and part of S1, S4.

- [ ] D2. `WikiModel` shares `cursor`, `scroll` and `input` across screens; an invariant is kept by hand. `internal/tui/wiki.go:61-125`

- [ ] D3. Two `:w`/`:q`/`:wq` implementations with different unsaved-change handling. `internal/tui/commands.go:129-163`, `internal/vim/ex.go:176-204`

- [ ] D4. Duplication and dead code: `plural`, `itoa`, aligned tables, `$VISUAL`/`$EDITOR` lookup, `_ = runes`, `ansiBlue`, `runNamed`. See `REVIEW.md` D4.

## User wiki

See `docs/dev/user-wiki.md`. Only `-u` opens the user wiki; nothing falls back to it.

- [x] U1. `-u` / `--user` before any command opens `~/.gwiki`, for every command and server (`ui`, `serve`, `mcp`, `lsp`); `gwiki -u init` creates it.
- [x] U2. Without `-u`, discovery stops at the repository root and never takes `~` for a project. `internal/wiki/project.go:61`
- [x] U3. The user wiki's repository is found from `~/.gwiki` upward. `internal/wiki/project.go:148`
- [x] U4. The interface header, `serve` and the browser view name the open wiki: "user" (from `Project.User`) or the project. No `--json` output carries a wiki name, so none was added.

## Ideas

- [ ] `-C <dir>`, as in `git -C`: open the project at a path instead of discovering one. Useful for scripts and per-project MCP registration.

- [ ] A view across projects: open tasks from every known project. It needs a list of known projects, which nothing records today.

- [ ] Show an unlabelled wiki link to a heading as `Drawing > Markdown`, as Obsidian does, not as written (`Drawing#Markdown`). The default label is set in the parser, so the terminal and browser views change together. `internal/markdown/wikilink.go:85-87`

- [ ] A backlink in the browser view scrolls to the link itself and highlights it. It now lands on the heading above, since the rendered page has no line numbers. Needs each link element marked with its source line (`data-line`), in the renderer, the page API and `app.js`. `internal/markdown/html.go`, `internal/webwiki/api.go` (`backHref`)

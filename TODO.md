# TODO

IDs refer to `REVIEW.md` (commit `2cdc3d9`, 2026-09-27), which holds the evidence and reproduction steps.

## Low

- [ ] P4. Indirect dependencies lag: `golang.org/x/text` v0.3.8, `go-runewidth` v0.0.16.

- [ ] W17. The tokenizer splits on combining marks; FTS5 does not. NFD queries find nothing. `search/search.go:78`

- [ ] W18. `FindTask` treats any `text:digits` as `page:line`. `ops.go:331-347`

- [ ] W20. A directory link indexed before its `README.md` exists stays `kind=file`. `nested.go:45-63`

- [ ] S10. Web server sets only `ReadHeaderTimeout`; no `IdleTimeout`, no graceful shutdown. `webwiki/server.go:205`

- [ ] S12. Web errors return absolute paths. `webwiki/api.go:303`

- [ ] S14. MCP has no frame size limit. `mcp/mcp.go:144-159`

- [ ] S15. A malformed `%` escape in the hash throws outside the `try`. `webwiki/assets/app.js:161-169`

- [ ] T15. `:s` replaces on the matched substring, so `\B` and similar assertions fail. `vim/ex.go:295`

- [ ] T16. A `\n` replacement shifts lines; the `:s` range does not follow. `vim/ex.go:283-303`

- [ ] T17. Only `/` separates `:s` parts. `vim/ex.go:129`

- [ ] T23. `orphans` and `tasks` ignore extra arguments; `search -n -5` is accepted. `cli/wiki.go:555-560`

- [ ] T24. `Refresh` and the draft write run inside `Update` each second. `tui/wiki.go:492-494`

- [ ] T25. Bidirectional overrides (U+202E) and zero-width characters pass the display filter. `display/display.go:30`

## Design

- [ ] D1. Move validation into `internal/wiki`: `Task` carries its text and is re-checked, mutators take a base hash, one symlink-safe open, link schemes classified once. The individual fixes (W4, W5, W8, S5) are done; this is the structural cleanup that keeps them from regressing.

- [ ] D2. `WikiModel` shares `cursor`, `scroll` and `input` across screens; an invariant is kept by hand. `internal/tui/wiki.go:61-125`

- [ ] D4. Duplication and dead code: `plural`, `itoa`, aligned tables, `$VISUAL`/`$EDITOR` lookup, `_ = runes`, `ansiBlue`, `runNamed`. See `REVIEW.md` D4.

## Ideas

- [ ] `-C <dir>`, as in `git -C`: open the project at a path instead of discovering one. Useful for scripts and per-project MCP registration.

- [ ] A view across projects: open tasks from every known project. It needs a list of known projects, which nothing records today.

- [ ] Show an unlabelled wiki link to a heading as `Drawing > Markdown`, as Obsidian does, not as written (`Drawing#Markdown`). The default label is set in the parser, so the terminal and browser views change together. `internal/markdown/wikilink.go:85-87`

- [ ] A backlink in the browser view scrolls to the link itself and highlights it. It now lands on the heading above, since the rendered page has no line numbers. Needs each link element marked with its source line (`data-line`), in the renderer, the page API and `app.js`. `internal/markdown/html.go`, `internal/webwiki/api.go` (`backHref`)

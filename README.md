# gwiki

A wiki of markdown pages kept in the repository it documents.

## Features

- **Plain files.** Pages are markdown in `.gwiki/wiki`, committed with the code. Any editor works.
- **User wiki.** Notes that belong to no project live in `~/.gwiki`, opened with `-u`.

- **Links into the code.** `[[wiki]]` and markdown links reach pages, headings, directories, source files and line ranges.

- **Link checking.** Broken links and line anchors whose code moved are reported, with repairs to choose from.

- **Moves keep links.** Moving a page rewrites every link to it; `--dry-run` lists them first.

- **Tasks.** Checklist items and task pages, with status, priority and due dates.

- **Search.** Ranked full-text search over titles, headings, tags and bodies.

- **Terminal interface.** An overview, a page tree, and a vim buffer that follows links and shows backlinks.

- **Language server.** Completion, diagnostics, go to definition, backlinks, rename and quick fixes in Neovim, Helix or Vim.

- **Browser view.** `gwiki serve` shows and edits the wiki in a browser, with nothing to install.

- **Agent server.** An MCP server lets a code agent read and edit pages without overwriting yours.

- **Command line.** Every operation, with `--json` on read commands.

- **Safe writes.** Writing a whole page that changed since it was read is refused, not merged.

- **One executable.** Pure Go, with builds for macOS, Linux and Windows.

## Overview

Pages live in `.gwiki/wiki` and are committed with the code. They link to each other with `[[Page title]]` or markdown links, and to source files and line ranges such as `../../src/lexer.go#L42`. gwiki indexes the links, headings, tags and tasks in a cache it rebuilds from the pages, reports broken links, and rewrites links when a page moves.

When the editor, the browser or an agent saves a whole page, gwiki first checks that the page has not changed since it was read. If it has, the save is refused, so one's edits are not overwritten. A command that changes only part of a page, such as `edit -m`, a tag, a task's status or a link repair, makes its change to the page as it is when the command runs.

![The gwiki overview on the latest tab: pages listed by title, path and when they changed](https://raw.githubusercontent.com/shakfu/gwiki/main/docs/media/gwiki-latest.png)

## Install

Download an archive for macOS arm64, Linux x64 or Windows x64 from the [releases](https://github.com/shakfu/gwiki/releases), check it against `SHA256SUMS`, and put `gwiki` on your `PATH`. The binaries are not signed; on macOS, clear the quarantine flag once with `xattr -d com.apple.quarantine gwiki`.

With Go:

```sh
go install github.com/shakfu/gwiki/cmd/gwiki@latest
```

Or from a clone: `make install`.

## Getting started

```sh
cd your-project
gwiki init                                  # .gwiki/wiki, committed; .gwiki/cache.db, ignored
gwiki new "Design sketch" --in lexer -m "The lexer tokenizes input. See [[Grammar]]."
gwiki                                       # the interface, on the overview
```

`init` also writes `.gwiki/config.json`, whose `name` is the project name the interface shows. It is the directory's name when `init` runs; edit it if you rename the directory.

## Project and user wikis

A project wiki lives in `.gwiki/wiki` in a repository and is committed with its code. gwiki finds it by walking up from the working directory, stopping at the repository root.

Your user wiki lives in `~/.gwiki/wiki`, for notes that belong to no project. Only `-u` (or `--user`), before any command, opens it:

```sh
gwiki -u init                               # create ~/.gwiki
gwiki -u new "Reading list" -m "..."
gwiki -u                                    # the interface, on the user wiki
gwiki -u mcp                                # an agent session on the user wiki
```

Nothing falls back to it. A command run where there is no project wiki reports "no gwiki found", so project notes never land in the user wiki by mistake. The two wikis do not link to each other. To version the user wiki, run `git init ~/.gwiki`, or keep it in a repository at `~`.

## Pages and links

A page is a markdown file under `.gwiki/wiki`; its path without `.md` names it, such as `lexer/design-sketch`. Its title is the front matter `title`, else its first level-one heading, else its file name. Front matter can also set `tags`, and `type: task` with `status`, `priority` and `due` for a task page. Checklist items (`- [ ] text`) in any page are tasks too.

| link | reaches |
|---|---|
| `[[Design sketch]]` | a page by path, then title, then file name |
| `[[lexer/design-sketch#Tokens\|the tokens]]` | a heading, with a label |
| `[notes](../grammar.md#rules)` | a page by relative path |
| `[[lexer]]`, `[lexer](lexer/)` | a directory's `README.md` |
| `[lexer](../../src/lexer.go#L42-L50)` | a file, or a line range in it |

A reference names a page by its path, its title, its file name, or a fragment; an ambiguous one lists the candidates.

Pages nest in directories to any depth. A directory's `README.md` is the directory's own page, as GitHub shows it when the directory is browsed: `[[lexer]]` and a link to `lexer/` reach it, it is titled by the directory's name when it has no title, and the tree draws it on the directory's row. This repository's `.gwiki/wiki` is an example: sections for guides, architecture, decisions and tasks, each with a README.

## The interface

Run `gwiki` with no arguments. It opens on the overview, whose header is a bar of three tabs; `tab` and `shift-tab` move between them:

- **latest**: pages by when they changed, with the author of the last commit, and pages git has not recorded;

- **tasks**: task pages and checklist items, overdue and due soon first;

- **stats**: broken links, orphan pages that nothing links to, dead ends that link to nothing, the most-linked pages, directories and tags.

Every row opens its page or list. `O` returns to the tab last shown, and `t` goes to tasks.

Opening a page shows the tree beside the page's markdown source, in a vim buffer. `tab` and `shift-tab` move between the tree, the page and its backlinks: the pages linking to it open under the page while they have the focus, and fold away when it leaves, so the page keeps the screen's height. The header bar counts them.

![A page open in gwiki: the page tree on the left, the page's markdown source in a vim buffer on the right, the cursor on a wiki link that the status bar resolves](https://raw.githubusercontent.com/shakfu/gwiki/main/docs/media/gwiki-wiki-editor.png)

In the tree, the overview and the lists:

| key | |
|---|---|
| `j` `k` `g` `G` | move |
| `enter` | open the page, list, or a directory's README |
| `space` | in the tree, fold a directory |
| `tab` `shift-tab` | on the overview, the next and previous tab |
| `l` `h`, `→` `←` | in the tree, unfold a directory or open a page; fold a directory or go to its parent. On stats, between columns |
| `/` | search every page as you type |
| `ctrl-p` | open a page by title or path |
| `n` `c` `t` `O` | new page, broken links, the tasks tab, the overview |
| `space` `a` `f` | in the tasks and broken-link lists: toggle a task, all or open tasks, repairs for a link |
| `R` | reload, for git details after a commit |
| `:` | a command |
| `?` | every key and command, starting with the current screen's |

### The page

The page is edited where it is read. Its buffer follows vim: modes, counts, `d`, `c`, `y` and `>` with motions and text objects, `.`, `u` and `ctrl-r`, registers, visual mode, `/` and `?` search, and `:s`. `il` and `al` select the link at the cursor, and `ctrl-space` ticks a checklist item. In normal mode the wiki takes a few keys:

| key | |
|---|---|
| `<` `>` | previous and next link; the status bar names its target |
| `enter` `ctrl-]` | follow the link under the cursor; a file link opens `$EDITOR` at the line |
| `ctrl-o` | back to the previous page and position |
| `[` `]` | half a screen up and down |
| `tab` `shift-tab` | the next and previous pane |
| `ctrl-p` | open a page |

`<` and `>` do not indent in normal mode; select lines with `V` and indent them there, or use `ctrl-t` and `ctrl-d` in insert mode.

Wiki actions are commands, typed in the page or after `:` elsewhere:

```text
:w  :e!         write the page; load it again and lose your edits
:q  :wq  :q!    quit gwiki; write and quit; quit and lose your edits
:new [title]    a page beside this one        :mv [path]   move it, rewriting links
:search text    search every page             :fix         repairs for the link under the cursor
:broken :tasks  broken links; tasks           :backlinks   to the backlinks panel
:preview        the page drawn as markdown    :check       the broken links in it
:external       edit in $EDITOR               :overview :reload :help
```

A page with unsaved changes is not left: `:w` writes it, `:e!` discards the edits. In insert mode, `enter` continues a list, numbering and checkboxes included, and `ctrl-n` completes a page after `[[`, a heading after `#`, or a path after `](`. `"+y` copies to the system clipboard through the terminal (OSC 52). The buffer is autosaved to `.gwiki/drafts/` and offered again if gwiki is interrupted. A write is refused when the page changed on disk since it was read.

Pages changed outside the interface, by an editor, git or an agent, reload within a second; a buffer with unsaved changes is marked instead.

Macros, marks, blockwise visual, `:g`, folds and mappings are not there; `:help` lists what is.

## Editors

`gwiki lsp` is a language server in the same executable. An editor starts it and gets, in wiki pages: `[[` and `](` completion of pages, headings and paths; warnings on broken links, and notes on line anchors whose code moved; go to definition to follow a link; references for backlinks; hover; heading outlines; rename of a page with its links rewritten; and quick fixes for broken links. Open buffers are checked as typed, before saving.

Neovim 0.11 or later:

```lua
vim.lsp.config('gwiki', {
  cmd = { 'gwiki', 'lsp' },
  filetypes = { 'markdown' },
  root_markers = { '.gwiki' },
})
vim.lsp.enable('gwiki')
```

Its default LSP keys then apply: `ctrl-]` follows a link, `grr` lists backlinks, `grn` renames the page, `gra` offers fixes, `K` hovers, `ctrl-x ctrl-o` completes.

Helix, in `.helix/languages.toml` or `~/.config/helix/languages.toml`:

```toml
[language-server.gwiki]
command = "gwiki"
args = ["lsp"]

[[language]]
name = "markdown"
language-servers = ["gwiki"]
```

Vim has no built-in LSP client; with [vim-lsp](https://github.com/prabirshrestha/vim-lsp):

```vim
au User lsp_setup call lsp#register_server({'name': 'gwiki', 'cmd': {server_info->['gwiki', 'lsp']}, 'allowlist': ['markdown']})
```

A rename returns edits for the editor to apply, so the pages it changes are left modified and unsaved; save them all (`:wall`).

## The browser view

```sh
gwiki serve
```

Opens the wiki in your browser: the same overview, the page tree, pages with their links and backlinks, search, broken links, tasks, and an editor. A link into the code opens that file at its lines. The view updates by itself when the command line, the terminal interface or an agent writes. The selector in the header chooses a light or dark theme, or follows the system's; the choice is remembered across runs.

The whole page is compiled into the binary, so there is nothing to install and it works with no network. The address carries an access token, and the API answers nothing without it: any page open in your browser can reach `127.0.0.1`, so the token, not the loopback binding, is the protection. `--no-open` prints the address without opening a browser, and `--addr` chooses the port. No browser opens over SSH, under CI, or on a Unix session with no display; `--open` forces it.

You edit a page in a text box. If the page was saved elsewhere since you opened it, your save is refused and the other save is kept. `make build-slim` (`-tags noweb`) leaves the browser view out.

## Agents

```sh
claude mcp add gwiki -- gwiki mcp
```

The agent gets tools to list, search and read pages, create them, edit exact text or write whole pages, rename pages, list and repair broken links and line anchors whose code moved, and change task status. If a page changed since the agent read it, the write is refused, and the agent gets the current page to try again. There is no delete tool.

## Commands

```sh
gwiki ls lexer                              # pages under a directory
gwiki show "design sketch"                  # a page, its links and backlinks
gwiki search tokeniz                        # ranked; the last word matches as a prefix
gwiki links index    gwiki backlinks grammar
gwiki check                                 # broken links and skipped files; exit status 1 while any remain
gwiki check --fix                           # choose a repair for each
gwiki check --strict                        # also exit 1 when line anchors drifted
gwiki orphans
gwiki new "Parser notes" --in lexer -t parser -m "First line."
gwiki new "Ship it" --task
gwiki edit "parser notes"                   # opens $EDITOR on the page
gwiki mv lexer/parser-notes archive/ --dry-run   # the links it would rewrite
gwiki export site                           # a copy with [[wiki]] links as markdown links, for GitHub
gwiki rm lexer/parser-notes                 # refused while pages link to it
gwiki tag grammar parser    gwiki untag grammar parser
gwiki tasks -s open
gwiki done index:12   gwiki doing tasks/ship   gwiki reopen index:12
gwiki promote index:12                      # a checklist item becomes a task page
gwiki cache --rebuild
gwiki serve                                 # the wiki in a browser
gwiki ui                                    # the terminal interface
gwiki lsp                                   # the language server, started by an editor
gwiki mcp                                   # the agent server, started by a client
```

Read commands take `--json`. [The design](docs/dev/wiki-design.md) covers the cache, link resolution and writes in detail.

gwiki follows no symlinks in `.gwiki/wiki`, and leaves out a page it cannot read. Every command warns of these files on standard error, and `gwiki check` lists them. A write that lands but cannot update the cache succeeds with a warning; the next refresh repairs the cache.

## Development

```sh
make test        # everything, with the race detector
make check       # vet, gofmt and test
make ci          # check, staticcheck and govulncheck, as CI runs them
make build-slim  # without the browser view
make bench
make cover
```

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## Licence

MIT. See [LICENSE](LICENSE).

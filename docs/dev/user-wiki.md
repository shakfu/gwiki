# User wiki

A second kind of wiki beside the project wiki: one per user, in `~/.gwiki`,
for notes that belong to no repository. Decided and built 2026-09-28.

## Terms

- **Project wiki**: `.gwiki/wiki` in a repository, committed with its code.
  What gwiki opens today.
- **User wiki**: `~/.gwiki/wiki`. It may be versioned on its own, with
  `git init ~/.gwiki`, or as part of a repository at `~`.

## Decided

- **Separate.** The user wiki does not link to project wikis, and they do not
  link to it. No shared index, no `[[user:...]]` link form. To revisit on
  demand.
- **`-u` / `--user` opens the user wiki**, before any command:
  `gwiki -u new "Idea"`, `gwiki -u ui`, `gwiki -u mcp`. "User" over the
  removed notes model's `-g` / `--global`: it names the scope, and pairs with
  "project". `-u` is free.
- **`gwiki -u init` creates it.**
- **Only `-u` opens it. Nothing falls back to it.** Without `-u`, gwiki opens
  the nearest `.gwiki/wiki` walking up from the working directory, stopping
  at the repository root (the first directory holding `.git`), or reports "no
  gwiki found". `~` is never taken for a project, so `~/.gwiki` is not found
  this way. A command run in a project that has no wiki therefore cannot
  write into the user wiki.
- **The same for every server.** `gwiki -u mcp` gives an agent the user wiki
  for that session; `gwiki mcp` never does. A client registration made once
  for every project therefore never exposes personal notes. An editor gets it
  from `gwiki -u lsp`, configured for `~/.gwiki`; `gwiki -u serve` shows it.
- **The user wiki's repository is found from `~/.gwiki` upward**, so both
  `~/.gwiki/.git` and a repository at `~` work. With neither, it is
  `~/.gwiki`, so file links and the browser's file panel do not reach the
  whole home directory.
- **The open wiki is always named**: the interface header, `serve` and the
  browser view show "user" for the user wiki, from `Project.User`, not from
  its editable `config.json`; else the project's name.

Explicit over a fallback to the user wiki where no project wiki is found:
someone who forgot `gwiki init` in a project would write its notes into their
user wiki without noticing. The removed `-g` design refused that case too.

## Open

- **Location override.** Tests can set `HOME`. An environment variable, such
  as `GWIKI_USER`, only if someone needs the wiki elsewhere.

## Related

- `TODO.md` Ideas: `-C <dir>`, as in `git -C`. `-u` is then `-C` to the user
  wiki's parent; build `-C` first if both are wanted.
- `TODO.md` Ideas: a view across projects needs a list of known wikis. The
  user wiki is a place to keep one, if that view is built.
- `docs/dev/mcp-base.md`: a destructive MCP tool takes a base hash; the same
  applies to the user wiki's MCP session.

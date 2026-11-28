# Quarto rendering

Measured with quarto 1.10.18 on macOS, November 2026.

## Quarto skips inputs under hidden directories

The same `_quarto.yml` (`output-dir`, `toc`, `pre-render`) and `sub/doc.qmd`, under two parents:

| project root | `quarto render sub/doc.qmd` | `quarto render` at the root |
|---|---|---|
| `visible/` | output in `_out/`, toc applied, `pre-render` ran | `_out/sub/doc.html` |
| `.hidden/` | output beside the source, project ignored | `pre-render` ran, nothing rendered, exit 0 |

The hidden component can be anywhere in the absolute path: `.gwiki/wiki/proj/_quarto.yml` also renders nothing. Every page is under `.gwiki`, so no page can be part of a project in place.

## `--output-dir`

- A standalone file accepts `--output-dir`.
- With any `_quarto.yml` above the file, including one the file is excluded from, quarto exits with `The --output-dir flag can only be used when rendering projects.` It still creates `.quarto/` at that project's root.
- `--output` refuses a path: `--output option cannot specify a relative or absolute path`.
- `quarto render --help` lists no option to ignore an enclosing project.

## Reported outputs

With two formats in `format:` and no `--to`, quarto wrote `doc.html` and `doc.docx` but printed `Output created:` for the first only. gwiki therefore lists outputs by comparing the staging directory before and after the render.

## Links

The export turns `[[Two]]` into `[Two](ch2.qmd)`. A `website` project rewrites that to `./ch2.html`; a `default` project and a standalone render leave `ch2.qmd`.

## `--metadata-file`

Tried as a shared-defaults file without a project: its `format: html: toc: true` applied, but a page's own `toc: false` did not override it. Not used.

## Design: staged rendering

`:quarto render` exports the saved pages, with `[[wiki]]` links as markdown links, to `$TMPDIR/gwiki-<uid>/<name>-<hash>/`, runs quarto there, and mirrors the result to `.gwiki/render/`.

- **Outputs** are found by comparing the staged files before and after the render. Exported inputs, `.quarto/` and `_freeze/` are not copied. A file the render deleted, as when quarto cleans its `output-dir`, is removed from `.gwiki/render/`.
- **The staging directory is kept,** so quarto's caches last between renders. On Unix its parent must be owned by the user and closed to others, since a shared `/tmp` could hold one planted by someone else. A path with a hidden component is refused, since quarto would render nothing there; on Linux that rules out `~/.cache`.
- **Rendering in place** was the alternative. Besides the hidden-directory problem, it writes output into the committed pages directory, and a `--to gfm` render of `a.qmd` writes an `a.md` that replaces the page.
- **A path that leaves the wiki,** such as `../../data.csv` in a code cell, does not resolve in the copy.

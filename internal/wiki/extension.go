package wiki

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// A page is a file ending in .md or, for Quarto, .qmd. Its path, the page's
// identity, leaves the extension out, so a.md and a.qmd would be one page: the
// .md is the page and the .qmd is skipped. A file ending in either in another
// case, such as Notes.MD, is skipped too: on a case-sensitive file system a
// save to Notes.md would be a second file.

// pageExts are the page extensions, in the order a page's file is looked up.
var pageExts = []string{".md", ".qmd"}

// ErrMisnamed is the reason a file ending in .md or .qmd in another case is
// skipped.
var ErrMisnamed = errors.New("not a page: the extension must be .md or .qmd")

// ErrShadowed is the reason a.qmd is skipped beside a.md.
var ErrShadowed = errors.New("not a page: a .md of the same name is the page")

// pageExt is the page extension name ends in, or "".
func pageExt(name string) string {
	for _, ext := range pageExts {
		if strings.HasSuffix(name, ext) {
			return ext
		}
	}
	return ""
}

// isPageFile reports whether name ends in a page extension.
func isPageFile(name string) bool { return pageExt(name) != "" }

// IsPageFile reports whether a file name is a page's: it ends in .md or .qmd.
func IsPageFile(name string) bool { return isPageFile(name) }

// pageID is the page a file relative to the pages directory holds.
func pageID(rel string) string { return strings.TrimSuffix(rel, pageExt(rel)) }

// trimPageExt removes a page extension in any case, for a name typed or
// linked rather than read from the disk.
func trimPageExt(s string) string {
	for _, ext := range pageExts {
		if len(s) > len(ext) && strings.EqualFold(s[len(s)-len(ext):], ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}

// misnamed reports whether name ends in a page extension in another case.
func misnamed(name string) bool {
	ext := path.Ext(name)
	for _, e := range pageExts {
		if ext != e && strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// Renamed returns the page file a misnamed file would be renamed to, and
// whether rel is misnamed. Both are relative to the pages directory.
func Renamed(rel string) (string, bool) {
	if !misnamed(rel) {
		return "", false
	}
	ext := path.Ext(rel)
	return rel[:len(rel)-len(ext)] + strings.ToLower(ext), true
}

// pageRelIn is a page's file relative to the pages directory: the first of its
// extensions that exists, or .md for a page not yet written. A name that
// cannot be checked counts as existing, so reading it reports why.
func pageRelIn(lstat func(string) (fs.FileInfo, error), page string) string {
	base := filepath.FromSlash(page)
	for _, ext := range pageExts {
		if _, err := lstat(base + ext); !errors.Is(err, fs.ErrNotExist) {
			return base + ext
		}
	}
	return base + ".md"
}

// FixExtension renames a misnamed file to its extension in lower case, making
// it a page, and rewrites the markdown links written to its old name. A line
// link is left as written, since #L2 on a page names a heading. It returns the
// links rewritten. A file already at the new name is never replaced.
func (w *Wiki) FixExtension(rel string) ([]Link, Warnings, error) {
	to, ok := Renamed(rel)
	if !ok || !fs.ValidPath(rel) {
		return nil, Warnings{}, fmt.Errorf("%s does not end in .md or .qmd in another case", rel)
	}
	root, err := w.root()
	if err != nil {
		return nil, Warnings{}, err
	}
	defer root.Close()
	if err := noSymlink(root, rel); err != nil {
		return nil, Warnings{}, err
	}
	from, err := root.Lstat(filepath.FromSlash(rel))
	if err != nil {
		return nil, Warnings{}, err
	}
	if !from.Mode().IsRegular() {
		return nil, Warnings{}, fmt.Errorf("%s is not a regular file", rel)
	}

	abs, err := filepath.Rel(w.Repo, filepath.Join(w.PagesPath(), filepath.FromSlash(rel)))
	if err != nil {
		return nil, Warnings{}, err
	}
	links, err := w.links(`kind = 'file' AND resolved = ?`, filepath.ToSlash(abs))
	if err != nil {
		return nil, Warnings{}, err
	}
	writes, rewritten, err := w.extensionWrites(links)
	if err != nil {
		return nil, Warnings{}, err
	}

	// A case-insensitive file system reports the new name as the file itself;
	// rename(2) changes its case. Otherwise a hard link fails rather than
	// replacing a file created at the new name since the check.
	if at, err := root.Lstat(filepath.FromSlash(to)); err == nil && os.SameFile(from, at) {
		err = root.Rename(filepath.FromSlash(rel), filepath.FromSlash(to))
		if err != nil {
			return nil, Warnings{}, err
		}
	} else {
		if err := root.Link(filepath.FromSlash(rel), filepath.FromSlash(to)); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil, Warnings{}, fmt.Errorf("%w: %s", ErrExists, to)
			}
			return nil, Warnings{}, err
		}
		if err := root.Remove(filepath.FromSlash(rel)); err != nil {
			return nil, Warnings{}, fmt.Errorf("%s is now also %s, but the old name remains: %w", rel, to, err)
		}
	}

	warn, err := w.commit(writes)
	if err != nil {
		return nil, warn, fmt.Errorf("renamed %s to %s, but its links were not rewritten: %w", rel, to, err)
	}
	return rewritten, warn, nil
}

// extensionWrites rewrites each link's destination to end in the lower-case
// extension. A link whose
// written path does not end in the extension, such as one percent-encoded, is
// left as written.
func (w *Wiki) extensionWrites(links []Link) ([]fileWrite, []Link, error) {
	byPage := map[string][]Link{}
	var order []string
	for _, l := range links {
		if l.Form != "markdown" || l.DestStart < 0 {
			continue
		}
		if byPage[l.Page] == nil {
			order = append(order, l.Page)
		}
		byPage[l.Page] = append(byPage[l.Page], l)
	}
	var writes []fileWrite
	var rewritten []Link
	for _, page := range order {
		src, hash, err := w.Read(page)
		if err != nil {
			return nil, nil, err
		}
		var spans []span
		for _, l := range byPage[page] {
			if err := checkDest(src, l); err != nil {
				return nil, nil, err
			}
			old := string(src[l.DestStart:l.DestEnd])
			p, rest, anchored := strings.Cut(old, "#")
			if anchored {
				rest = "#" + rest
			}
			if !misnamed(p) {
				continue
			}
			ext := path.Ext(p)
			spans = append(spans, span{start: l.DestStart, end: l.DestEnd, old: old, new: p[:len(p)-len(ext)] + strings.ToLower(ext) + rest})
			rewritten = append(rewritten, l)
		}
		if len(spans) == 0 {
			continue
		}
		out, err := applySpans(src, spans)
		if err != nil {
			return nil, nil, err
		}
		writes = append(writes, fileWrite{Page: page, Base: hash, Data: out})
	}
	return writes, rewritten, nil
}

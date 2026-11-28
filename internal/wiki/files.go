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

// ErrNotAsset refuses a page written as a plain file: pages are written by
// Write, which checks the page has not changed since it was read.
var ErrNotAsset = errors.New("is a page; write pages with the page tools")

// TemplatesPath is the project's templates directory.
func (p *Project) TemplatesPath() string { return filepath.Join(p.Root, DirName, TemplatesDir) }

// cleanFile validates a file path relative to a directory of the wiki:
// slash-separated, inside it, with no hidden component.
func cleanFile(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if !fs.ValidPath(rel) || rel == "." {
		return "", fmt.Errorf("%q is not a path inside the directory", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%q has a hidden component, which the wiki skips", rel)
		}
	}
	return rel, nil
}

// PutFile writes a file that is not a page, such as an image, at rel in the
// pages directory. An existing file is replaced only with replace.
func (w *Wiki) PutFile(rel string, data []byte, replace bool) error {
	rel, err := cleanFile(rel)
	if err != nil {
		return err
	}
	if isPageFile(rel) || misnamed(rel) {
		return fmt.Errorf("%s %w", rel, ErrNotAsset)
	}
	if err := writeIn(w.PagesPath(), rel, data, replace); err != nil {
		return err
	}
	_, err = w.Refresh()
	return err
}

// PutTemplate writes a template, <name>.md or <name>.qmd, in .gwiki/templates.
func (w *Wiki) PutTemplate(rel string, data []byte) error {
	rel, err := cleanFile(rel)
	if err != nil {
		return err
	}
	if strings.Contains(rel, "/") || !isPageFile(rel) || !templateName.MatchString(pageID(rel)) {
		return fmt.Errorf("%q is not a template file: <name>.md or <name>.qmd", rel)
	}
	return writeIn(w.TemplatesPath(), rel, data, true)
}

// WritePageFile writes a page by its file, rel in the pages directory, as an
// agent names it: a new page takes the file's extension, and an existing one
// is replaced only while it is what is on disk now. a.qmd beside a.md is not
// the page, so writing it is refused.
func (w *Wiki) WritePageFile(rel string, data []byte) (Warnings, error) {
	rel, err := cleanFile(rel)
	if err != nil {
		return Warnings{}, err
	}
	if !isPageFile(rel) {
		return Warnings{}, fmt.Errorf("%s is not a page: it does not end in .md or .qmd", rel)
	}
	page := pageID(rel)
	_, hash, err := w.Read(page)
	switch {
	case err == nil:
		if have := filepath.ToSlash(w.file(page)); !strings.HasSuffix(have, "/"+rel) {
			return Warnings{}, fmt.Errorf("%s is not the page %s, which is %s", rel, page, path.Base(have))
		}
	case errors.Is(err, ErrSymlink):
		return Warnings{}, err
	default:
		hash = "" // a new page; commit refuses it if one appeared meanwhile
	}
	return w.commit([]fileWrite{{Page: page, Base: hash, Data: data, Ext: pageExt(rel)}})
}

// writeIn writes data at rel under dir through an os.Root, which no symlink
// can lead out of, staged in a temporary file and renamed into place.
func writeIn(dir, rel string, data []byte, replace bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	native := filepath.FromSlash(rel)
	if err := noSymlink(root, native); err != nil {
		return err
	}
	if info, err := root.Lstat(native); err == nil {
		if !replace {
			return fmt.Errorf("%s exists; replace it to overwrite", rel)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := stage(root, native, native, data)
	if err != nil {
		return err
	}
	if err := root.Rename(tmp, native); err != nil {
		root.Remove(tmp)
		return err
	}
	return nil
}

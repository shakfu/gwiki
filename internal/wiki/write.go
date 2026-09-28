package wiki

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/shakfu/gwiki/internal/markdown"
	"gopkg.in/yaml.v3"
)

// Hash is the content hash writes are checked against.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ErrConflict reports that a page changed after the writer read it. Nothing
// was written.
type ErrConflict struct {
	Page string

	// Current is the page's content now, and CurrentHash its hash, for the
	// writer to retry against. Both are empty when the page no longer exists.
	Current     []byte
	CurrentHash string
}

func (e *ErrConflict) Error() string {
	if e.CurrentHash == "" {
		return fmt.Sprintf("%s was removed after it was read; nothing was written", e.Page)
	}
	return fmt.Sprintf("%s changed after it was read; nothing was written", e.Page)
}

// ErrExists reports a page that already exists where a new one would go.
var ErrExists = errors.New("a page already exists there")

// ErrSymlink reports a symlink where a page or its directory would be. The
// wiki follows none: one out of the wiki would read and write outside it, and
// one inside makes two pages of one file, which a write turns back into two.
var ErrSymlink = errors.New("a symlink, which the wiki does not follow")

// ErrPartial reports a batch that stopped part-way: the pages in Done were
// written or removed, those in NotDone were not.
type ErrPartial struct {
	Done, NotDone []string
	Err           error
}

func (e *ErrPartial) Error() string {
	return fmt.Sprintf("only part of the change was made: %s done, %s not: %v",
		strings.Join(e.Done, ", "), strings.Join(e.NotDone, ", "), e.Err)
}

func (e *ErrPartial) Unwrap() error { return e.Err }

// Warnings are what a write that landed left wrong. The zero value is none.
type Warnings struct {
	// Stale is why the cache could not be refreshed after the write. It lags
	// the pages until a refresh succeeds.
	Stale error

	// Skipped are the files the wiki leaves out; see Wiki.Skipped.
	Skipped []Skip
}

// Empty reports whether there is nothing to warn about.
func (w Warnings) Empty() bool { return w.Stale == nil && len(w.Skipped) == 0 }

// String is the warnings on one line.
func (w Warnings) String() string {
	var parts []string
	if w.Stale != nil {
		parts = append(parts, fmt.Sprintf("the page was written but the cache was not updated (%v); if this persists, run gwiki cache --rebuild", w.Stale))
	}
	if n := len(w.Skipped); n == 1 {
		parts = append(parts, fmt.Sprintf("%s is not in the wiki: %s", w.Skipped[0].Path, w.Skipped[0].Reason))
	} else if n > 1 {
		parts = append(parts, fmt.Sprintf("%d files are not in the wiki; gwiki check lists them", n))
	}
	return strings.Join(parts, "; ")
}

// fileWrite is one change in a batch: new content for a page, or its removal
// when Data is nil. Base is the hash the writer read, or empty for a page that
// must not exist yet.
type fileWrite struct {
	Page string
	Base string
	Data []byte
}

// Read returns a page's source and its hash.
func (w *Wiki) Read(page string) ([]byte, string, error) {
	src, err := w.readPage(page)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no page %q", page)
		}
		return nil, "", err
	}
	return src, Hash(src), nil
}

func (w *Wiki) file(page string) string {
	return filepath.Join(w.PagesPath(), pageRel(page))
}

// pageRel is a page's file relative to the pages directory.
func pageRel(page string) string { return filepath.FromSlash(page) + ".md" }

// root opens the pages directory. Pages are read and written through it, so
// even a symlink that noSymlink missed cannot lead outside the directory.
func (w *Wiki) root() (*os.Root, error) { return os.OpenRoot(w.PagesPath()) }

// noSymlink refuses a path with a symlink in it. A missing part ends the check,
// since what does not exist yet is created as a directory or file.
func noSymlink(root *os.Root, rel string) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := range parts {
		p := filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		info, err := root.Lstat(p)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is %w", filepath.ToSlash(p), ErrSymlink)
		}
	}
	return nil
}

func (w *Wiki) readPage(page string) ([]byte, error) {
	root, err := w.root()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return readIn(root, page)
}

func readIn(root *os.Root, page string) ([]byte, error) {
	if err := noSymlink(root, pageRel(page)); err != nil {
		return nil, err
	}
	return root.ReadFile(pageRel(page))
}

// pageExists reports whether a page's file exists. An error other than "not
// exist", such as a name too long or a symlink out of the wiki, is returned.
func (w *Wiki) pageExists(page string) (bool, error) {
	root, err := w.root()
	if err != nil {
		return false, err
	}
	defer root.Close()
	if err := noSymlink(root, pageRel(page)); err != nil {
		return false, err
	}
	_, err = root.Stat(pageRel(page))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// commit applies a batch of writes, then refreshes the cache. An error means
// nothing was written, or, as ErrPartial, part of the batch; warnings come
// with a write that landed.
//
// Every base is checked before anything is written, under the cache's write
// lock, so gwiki processes writing at once take turns and a batch never lands
// half-checked. Every page is then staged in a synced temporary file before any
// is replaced, so a failure to write one leaves all as they were. The renames
// put new pages first and removals last: a move that stops part-way leaves the
// page in both places, never in neither. An editor outside gwiki takes no lock,
// so each base is checked again just before its rename; a save in the moment
// between that check and the rename is still overwritten.
func (w *Wiki) commit(writes []fileWrite) (Warnings, error) {
	root, err := w.root()
	if err != nil {
		return Warnings{}, err
	}
	defer root.Close()
	tx, err := w.db.Begin()
	if err != nil {
		return Warnings{}, fmt.Errorf("lock %s: %w", w.Cache(), err)
	}
	defer tx.Rollback()

	for _, fw := range writes {
		if err := checkBase(root, fw); err != nil {
			return Warnings{}, err
		}
	}

	writes = slices.Clone(writes)
	slices.SortStableFunc(writes, func(a, b fileWrite) int { return writeOrder(a) - writeOrder(b) })
	temps := make([]string, len(writes))
	defer func() {
		for _, t := range temps {
			if t != "" {
				root.Remove(t)
			}
		}
	}()
	for i, fw := range writes {
		if fw.Data != nil {
			if temps[i], err = stage(root, pageRel(fw.Page), fw.Data); err != nil {
				return Warnings{}, err
			}
		}
	}
	afterStage()
	done := 0
	for i, fw := range writes {
		// Checked again: an editor outside gwiki may have saved since, and
		// this leaves it one rename to do so unseen, not the whole batch.
		if err = checkBase(root, fw); err != nil {
			break
		}
		if fw.Data == nil {
			err = root.Remove(pageRel(fw.Page))
		} else if err = root.Rename(temps[i], pageRel(fw.Page)); err == nil {
			temps[i] = ""
		}
		if err != nil {
			break
		}
		done++
	}
	tx.Rollback()

	// Refreshed after a partial write too, so the cache matches the disk.
	var warn Warnings
	if _, warn.Stale = w.Refresh(); warn.Stale == nil {
		warn.Skipped, warn.Stale = w.Skipped()
	}
	switch {
	case done == 0 && err != nil:
		return Warnings{}, err
	case err != nil:
		partial := &ErrPartial{Err: err}
		for i, fw := range writes {
			if i < done {
				partial.Done = append(partial.Done, fw.Page)
			} else {
				partial.NotDone = append(partial.NotDone, fw.Page)
			}
		}
		return warn, partial
	}
	return warn, nil
}

// afterStage runs between staging and the renames; a test writes there as an
// outside editor would.
var afterStage = func() {}

// checkBase refuses a write whose page no longer has the hash it was read at,
// or, for a new page, one that exists.
func checkBase(root *os.Root, fw fileWrite) error {
	current, err := readIn(root, fw.Page)
	switch {
	case os.IsNotExist(err):
		if fw.Base != "" {
			return &ErrConflict{Page: fw.Page}
		}
	case err != nil:
		return err
	case fw.Base == "":
		return fmt.Errorf("%w: %s", ErrExists, fw.Page)
	case Hash(current) != fw.Base:
		return &ErrConflict{Page: fw.Page, Current: current, CurrentHash: Hash(current)}
	}
	return nil
}

// writeOrder ranks a write: new pages, then changed pages, then removals.
func writeOrder(fw fileWrite) int {
	switch {
	case fw.Data == nil:
		return 2
	case fw.Base == "":
		return 0
	}
	return 1
}

// stage writes data to a synced temporary file beside rel, with the mode of
// the page it will replace, and returns its name. The name is hidden, so a
// scan never reads it.
func stage(root *os.Root, rel string, data []byte) (string, error) {
	dir := filepath.Dir(rel)
	if err := root.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if info, err := root.Stat(rel); err == nil {
		mode = info.Mode().Perm()
	}
	var f *os.File
	var tmp string
	for {
		tmp = filepath.Join(dir, "."+filepath.Base(rel)+"."+strconv.FormatUint(rand.Uint64(), 36))
		var err error
		if f, err = root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
			break
		} else if !os.IsExist(err) {
			return "", err
		}
	}
	_, err := f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Chmod(tmp, mode)
	}
	if err != nil {
		root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// CleanPath validates a page path typed by a person or agent and returns it
// relative to the pages directory, slash-separated, without .md.
func CleanPath(p string) (string, error) {
	p = strings.TrimSuffix(strings.TrimSpace(filepath.ToSlash(p)), ".md")
	clean := path.Clean(p)
	switch {
	case p == "" || clean == ".":
		return "", errors.New("a page path is required")
	case path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../"):
		return "", fmt.Errorf("%q is outside the wiki", p)
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%q has a hidden component, which the wiki skips", p)
		}
		if strings.ContainsFunc(part, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
			return "", fmt.Errorf("%q contains a control character or backslash", p)
		}
	}
	return clean, nil
}

// Slugify makes a file name from a title: lowercase letters and digits, with
// every run of anything else as one hyphen.
func Slugify(title string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
			continue
		}
		hyphen = true
	}
	return b.String()
}

// span is a replacement of src[start:end], whose current text is old.
type span struct {
	start, end int
	old, new   string
}

// applySpans rewrites src. Spans must not overlap; each is checked against the
// text it expects, so a stale offset is an error rather than a corrupt page.
func applySpans(src []byte, spans []span) ([]byte, error) {
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	out := bytes.Clone(src)
	last := len(src) + 1
	for _, s := range spans {
		if s.start < 0 || s.end > len(src) || s.start > s.end || s.end > last {
			return nil, fmt.Errorf("edit at %d-%d does not fit the page", s.start, s.end)
		}
		if string(src[s.start:s.end]) != s.old {
			return nil, fmt.Errorf("expected %q at byte %d, found %q; refresh and retry", s.old, s.start, src[s.start:s.end])
		}
		out = append(out[:s.start], append([]byte(s.new), out[s.end:]...)...)
		last = s.start
	}
	return out, nil
}

// editFront rewrites a page's front matter through a YAML node tree, which
// keeps key order, styles and comments where yaml.v3 can. A page without
// front matter gets a new block.
func editFront(src []byte, edit func(m *yaml.Node) error) ([]byte, error) {
	page := markdown.Parse(src)
	var doc yaml.Node
	start, end := 0, 0
	if page.Front != nil {
		lines := bytes.SplitAfterN(src, []byte("\n"), 2)
		start = len(lines[0])
		end = bytes.LastIndex(src[:page.BodyStart], []byte("\n---"))
		if alt := bytes.LastIndex(src[:page.BodyStart], []byte("\n...")); alt > end {
			end = alt
		}
		end++ // just past the newline before the closing delimiter
		if err := yaml.Unmarshal(src[start:end], &doc); err != nil {
			return nil, err
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if err := edit(doc.Content[0]); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	enc.Close()
	block := buf.Bytes()
	if len(doc.Content[0].Content) == 0 {
		block = nil
	}

	var out bytes.Buffer
	if page.Front != nil {
		if block == nil {
			// Empty front matter is dropped, delimiters and the blank line
			// that followed them included.
			return bytes.TrimLeft(src[page.BodyStart:], "\n"), nil
		}
		out.Write(src[:start])
		out.Write(block)
		out.Write(src[end:])
		return out.Bytes(), nil
	}
	if block == nil {
		return src, nil
	}
	out.WriteString("---\n")
	out.Write(block)
	out.WriteString("---\n\n")
	out.Write(src)
	return out.Bytes(), nil
}

// mapValue returns the value node for key in a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setScalar sets key to value in a mapping, adding the key when missing.
func setScalar(m *yaml.Node, key, value string) {
	if v := mapValue(m, key); v != nil {
		v.Kind, v.Tag, v.Value, v.Content = yaml.ScalarNode, "!!str", value, nil
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// deleteKey removes key from a mapping.
func deleteKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

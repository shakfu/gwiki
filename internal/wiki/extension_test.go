package wiki

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A file ending in .MD is skipped and reported, its directory holds no pages,
// and FixExtension makes it a page, rewriting file links but not line links.
func TestMisnamedPageIsReportedAndRenamed(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	write(t, filepath.Join(pages, "notes", "B.MD"), "# B\n")
	put(t, w, root, map[string]string{"a": "[f](notes/B.MD) [l](notes/B.MD#L1) [d](notes/)\n"})

	skipped, err := w.Skipped()
	if err != nil || len(skipped) != 1 || skipped[0].Path != "notes/B.MD" || skipped[0].Reason != ErrMisnamed.Error() {
		t.Fatalf("Skipped = %+v, %v", skipped, err)
	}
	if _, err := w.Page("notes/B"); err == nil {
		t.Fatal("notes/B.MD was indexed")
	}
	links, err := w.Links("a")
	if err != nil || len(links) != 3 || links[2].Kind != KindFile || links[2].Status != StatusOK {
		t.Fatalf("a directory of .MD files is a section: %+v, %v", links, err)
	}

	rewritten, warn, err := w.FixExtension("notes/B.MD")
	if err != nil || len(rewritten) != 1 || len(warn.Skipped) != 0 {
		t.Fatalf("FixExtension = %+v, %+v, %v", rewritten, warn, err)
	}
	if got := source(t, root, "a"); got != "[f](notes/B.md) [l](notes/B.MD#L1) [d](notes/)\n" {
		t.Fatalf("a = %q", got)
	}
	if _, err := w.Page("notes/B"); err != nil {
		t.Fatalf("notes/B after the rename: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(pages, "notes"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "B.md" {
		t.Fatalf("notes/ = %v, %v", entries, err)
	}

	if _, _, err := w.FixExtension("a.md"); err == nil {
		t.Fatal("FixExtension renamed a page")
	}
}

// On a case-sensitive file system Notes.md and Notes.MD are two files; the
// rename refuses rather than replace the page.
func TestMisnamedPageRenameKeepsAnExistingPage(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	write(t, filepath.Join(pages, "c.md"), "# Mine\n")
	write(t, filepath.Join(pages, "c.MD"), "# Other\n")
	lower, err1 := os.Stat(filepath.Join(pages, "c.md"))
	upper, err2 := os.Stat(filepath.Join(pages, "c.MD"))
	if err1 != nil || err2 != nil || os.SameFile(lower, upper) {
		t.Skip("the file system ignores case")
	}
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.FixExtension("c.MD"); !errors.Is(err, ErrExists) {
		t.Fatalf("FixExtension = %v, want ErrExists", err)
	}
	if got := source(t, root, "c"); got != "# Mine\n" {
		t.Fatalf("c.md = %q", got)
	}
}

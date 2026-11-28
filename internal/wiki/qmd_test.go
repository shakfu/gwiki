package wiki

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A .qmd is a page: indexed, linked by [[name]] and by its file, written in
// place, its categories counted as tags, and moved with its extension.
func TestQmdIsAPage(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	write(t, filepath.Join(pages, "notes", "analysis.qmd"), "---\ntitle: Analysis\ncategories: [Data Science, ml]\ntags: [ml]\n---\n\n# Analysis\n\n```{python}\nprint(1)\n```\n")
	put(t, w, root, map[string]string{"index": "[[Analysis]] [file](notes/analysis.qmd) [heading](notes/analysis.qmd#analysis)\n"})

	info, err := w.Find("notes/analysis.qmd")
	if err != nil || info.Path != "notes/analysis" || info.Ext != ".qmd" || info.File() != ".gwiki/wiki/notes/analysis.qmd" {
		t.Fatalf("Find = %+v, %v", info, err)
	}
	if !slices.Equal(info.Tags, []string{"data science", "ml"}) {
		t.Errorf("Tags = %v", info.Tags)
	}
	links, err := w.Links("index")
	if err != nil || len(links) != 3 {
		t.Fatalf("Links = %+v, %v", links, err)
	}
	for _, l := range links {
		if l.Resolved != "notes/analysis" || l.Status != StatusOK {
			t.Errorf("%s: %s %s", l.Written(), l.Resolved, l.Status)
		}
	}

	_, hash, err := w.Read("notes/analysis")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write("notes/analysis", []byte("# Analysis, edited\n"), hash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pages, "notes", "analysis.md")); !os.IsNotExist(err) {
		t.Fatalf("a write made a .md beside the .qmd: %v", err)
	}

	if _, _, err := w.Move("notes/analysis", "reports/analysis"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(pages, "reports", "analysis.qmd")); err != nil || string(raw) != "# Analysis, edited\n" {
		t.Fatalf("moved = %q, %v", raw, err)
	}
	if got := source(t, root, "index"); !strings.Contains(got, "(reports/analysis.qmd)") || !strings.Contains(got, "(reports/analysis.qmd#analysis)") {
		t.Fatalf("index after the move: %q", got)
	}
}

// a.md is the page and a.qmd beside it is skipped; removing a.md makes a.qmd
// the page, as a change to a, not a removal and an addition.
func TestMdShadowsQmd(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	write(t, filepath.Join(pages, "a.qmd"), "# From qmd\n")
	put(t, w, root, map[string]string{"a": "# From md\n"})

	if p, err := w.Page("a"); err != nil || p.Title != "From md" {
		t.Fatalf("Page(a) = %+v, %v", p, err)
	}
	skipped, _ := w.Skipped()
	if len(skipped) != 1 || skipped[0].Path != "a.qmd" || skipped[0].Reason != ErrShadowed.Error() {
		t.Fatalf("Skipped = %+v", skipped)
	}
	if _, ok := w.PageOf(filepath.Join(pages, "a.qmd")); ok {
		t.Error("PageOf(a.qmd) names the page a.md holds")
	}

	if err := os.Remove(filepath.Join(pages, "a.md")); err != nil {
		t.Fatal(err)
	}
	ch, err := w.Refresh()
	if err != nil || !slices.Equal(ch.Modified, []string{"a"}) || len(ch.Added)+len(ch.Removed) != 0 {
		t.Fatalf("Refresh = %+v, %v", ch, err)
	}
	if p, err := w.Page("a"); err != nil || p.Title != "From qmd" || p.Ext != ".qmd" {
		t.Fatalf("Page(a) = %+v, %v", p, err)
	}
	if skipped, _ := w.Skipped(); len(skipped) != 0 {
		t.Fatalf("Skipped = %+v", skipped)
	}
}

// Removing a tag removes it from categories too, keeping the others as
// written; adding one already a category changes nothing.
func TestTagEditsCategories(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	write(t, filepath.Join(pages, "a.qmd"), "---\ncategories: [Data Science, ML]\n---\n")
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tag("a", []string{"data science"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tag("a", []string{"new"}, []string{"ml"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pages, "a.qmd"))
	if string(raw) != "---\ncategories: [Data Science]\ntags: [new]\n---\n" {
		t.Fatalf("a.qmd = %q", raw)
	}
}

// A second export leaves unchanged files alone, so their times stay, and
// lists every file it holds.
func TestExportKeepsUnchangedFiles(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n", "b": "# B\n"})
	out := t.TempDir()
	if _, err := w.Export(out); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(out, "a.md"), old, old); err != nil {
		t.Fatal(err)
	}
	write(t, pageFile(root, "b"), "# B, changed\n")
	exp, err := w.Export(out)
	if err != nil || !slices.Equal(exp.Written, []string{"a.md", "b.md"}) {
		t.Fatalf("Export = %+v, %v", exp, err)
	}
	if info, _ := os.Stat(filepath.Join(out, "a.md")); !info.ModTime().Equal(old) {
		t.Errorf("a.md was rewritten")
	}
	if raw, _ := os.ReadFile(filepath.Join(out, "b.md")); string(raw) != "# B, changed\n" {
		t.Errorf("b.md = %q", raw)
	}
}

// a.qmd beside a.md is exported as it is, without a.md's link offsets.
func TestExportCopiesAShadowedQmd(t *testing.T) {
	w, root := emptyWiki(t)
	qmd := "---\ntitle: Longer front matter\n---\n\nAnd [[b]] at another offset.\n"
	write(t, filepath.Join(root, DirName, PagesDir, "a.qmd"), qmd)
	put(t, w, root, map[string]string{"a": "# A\n\nsee [[b]] here\n", "b": "# B\n"})
	out := t.TempDir()
	exp, err := w.Export(out)
	if err != nil || exp.Pages != 2 || exp.Files != 1 {
		t.Fatalf("Export = %+v, %v", exp, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(out, "a.qmd")); string(raw) != qmd {
		t.Errorf("a.qmd = %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(out, "a.md")); !strings.Contains(string(raw), "[b](b.md)") {
		t.Errorf("a.md = %q", raw)
	}
}

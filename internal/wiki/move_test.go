package wiki

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A page's wiki links to itself are rewritten, and the plan lists them.
func TestMoveRewritesAPagesWikiLinksToItself(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"lexer/design-sketch": "# Design sketch\n\n## Tokens\n\n[[design-sketch#Tokens]] [[lexer/design-sketch]] [[#Tokens]] [[Design sketch]]\n",
	})
	plan, err := w.PlanMove("lexer/design-sketch", "archive/sketch")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 2 || plan.Edits[0].Page != "archive/sketch" {
		t.Fatalf("plan edits = %+v, want the two self links", plan.Edits)
	}
	if _, _, err := w.Move("lexer/design-sketch", "archive/sketch"); err != nil {
		t.Fatal(err)
	}
	want := "# Design sketch\n\n## Tokens\n\n[[sketch#Tokens|design-sketch#Tokens]] [[sketch|lexer/design-sketch]] [[#Tokens]] [[Design sketch]]\n"
	if got := source(t, root, "archive/sketch"); got != want {
		t.Fatalf("after the move:\n%s\nwant:\n%s", got, want)
	}
	for _, s := range statuses(t, w, "archive/sketch") {
		if !strings.Contains(s, " ok ") {
			t.Errorf("link %s", s)
		}
	}
}

// An untitled page is titled by its file name, so a move changes its title and
// links by the old one are rewritten.
func TestMoveOfAnUntitledPageRewritesLinksByItsName(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"notes/my-page": "No heading here. Self: [[my-page]]\n",
		"index":         "# Index\n\n[[my-page]] [[notes/my-page]]\n",
	})
	res, _, err := w.Move("notes/my-page", "archive/renamed")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Broken) != 0 {
		t.Fatalf("Broken = %+v", res.Broken)
	}
	if got, want := source(t, root, "index"), "# Index\n\n[[renamed|my-page]] [[renamed|notes/my-page]]\n"; got != want {
		t.Fatalf("index:\n%s\nwant:\n%s", got, want)
	}
	if got, want := source(t, root, "archive/renamed"), "No heading here. Self: [[renamed|my-page]]\n"; got != want {
		t.Fatalf("moved page:\n%s\nwant:\n%s", got, want)
	}
}

// A moved page keeps its file mode, and a directory the move empties is removed.
func TestMoveKeepsTheModeAndRemovesAnEmptiedDirectory(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"notes/deep/a": "# A\n", "keep/b": "# B\n"})
	if err := os.Chmod(pageFile(root, "notes/deep/a"), 0o600); err != nil {
		t.Fatal(err)
	}
	var synced []string
	syncDir = func(_ *os.Root, dir string) error {
		synced = append(synced, filepath.ToSlash(dir))
		return nil
	}
	t.Cleanup(func() { syncDir = syncDirOf })
	if _, warn, err := w.Move("notes/deep/a", "archive/a"); err != nil || !warn.Empty() {
		t.Fatalf("Move: %v, %v", warn, err)
	}
	info, err := os.Stat(pageFile(root, "archive/a"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("moved page: %v, %v; want mode 0600", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, DirName, PagesDir, "notes")); !os.IsNotExist(err) {
		t.Fatalf("notes is still there: %v", err)
	}
	slices.Sort(synced)
	if got := strings.Join(slices.Compact(synced), " "); got != ". archive" {
		t.Fatalf("synced %q, want the directories the move changed", got)
	}

	// A failed sync is a warning: the page was written.
	syncDir = func(*os.Root, string) error { return errors.New("no sync here") }
	_, warn, err := w.Move("keep/b", "keep/c")
	if err != nil || warn.Unsynced == nil || !strings.Contains(warn.String(), "no sync here") {
		t.Fatalf("Move with a failed sync: %+v, %v", warn, err)
	}
	if _, err := os.Stat(filepath.Join(root, DirName, PagesDir, "keep")); err != nil {
		t.Fatalf("keep was removed: %v", err)
	}
}

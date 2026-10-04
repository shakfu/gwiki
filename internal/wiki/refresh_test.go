package wiki

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// A same-size edit that keeps the old modification time, as rsync -t or tar
// leave one, is still re-indexed.
func TestRefreshSeesAnEditThatKeepsSizeAndTime(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# Alpha\n"})
	file := pageFile(root, "a")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if changeTime(info) == 0 {
		t.Skip("no change time on this platform")
	}
	time.Sleep(10 * time.Millisecond) // past the change time's resolution
	write(t, file, "# Gamma\n")
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	ch, err := w.Refresh()
	if err != nil || strings.Join(ch.Modified, ",") != "a" {
		t.Fatalf("Refresh = %+v, %v; want a modified", ch, err)
	}
	if pages, _ := w.Pages("", ""); len(pages) != 1 || pages[0].Title != "Gamma" {
		t.Fatalf("pages = %+v", pages)
	}

	// A change of mode alone moves the change time but not the content.
	time.Sleep(10 * time.Millisecond)
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if ch, err := w.Refresh(); err != nil || !ch.Empty() {
		t.Fatalf("Refresh after chmod = %+v, %v; want nothing changed", ch, err)
	}
	if stored, _ := storedFingerprint(w.db); stored == "" || strings.HasPrefix(stored, "retry") {
		t.Fatalf("fingerprint = %q", stored)
	}
	var ctime int64
	if err := w.db.QueryRow(`SELECT ctime FROM files WHERE path = 'a.md'`).Scan(&ctime); err != nil {
		t.Fatal(err)
	}
	now, _ := os.Stat(file)
	if ctime != changeTime(now) {
		t.Fatalf("stored change time %d, file has %d", ctime, changeTime(now))
	}
}

// A page removed between the scan and the read is treated as removed, not
// listed as a file the wiki could not read.
func TestAPageRemovedDuringARefreshIsRemoved(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n"})
	write(t, pageFile(root, "a"), "# A changed\n")
	touch(t, pageFile(root, "a"))
	write(t, pageFile(root, "b"), "# B\n")
	beforeParse = func() {
		os.Remove(pageFile(root, "a"))
		os.Remove(pageFile(root, "b"))
	}
	t.Cleanup(func() { beforeParse = nil })
	ch, err := w.Refresh()
	if err != nil || strings.Join(ch.Removed, ",") != "a" || len(ch.Added) != 0 {
		t.Fatalf("Refresh = %+v, %v; want a removed", ch, err)
	}
	if skipped, err := w.Skipped(); err != nil || len(skipped) != 0 {
		t.Fatalf("skipped = %+v, %v", skipped, err)
	}
}

// Content shared by several removed or added pages names no rename, and the
// renames kept are bounded.
func TestRenameDetectionNeedsOneCandidate(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"x": "# Same\n", "y": "# Same\n", "solo": "# Solo\n"})
	for _, id := range []string{"x", "y", "solo"} {
		os.Remove(pageFile(root, id))
	}
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	ch, err := w.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	put(t, w, root, map[string]string{"z": "# Same\n", "solo-moved": "# Solo\n"})
	renames, err := w.Renames()
	if err != nil || len(renames) != 1 || renames[0] != [2]string{"solo", "solo-moved"} {
		t.Fatalf("renames = %v, %v (%+v)", renames, err, ch)
	}

	put(t, w, root, map[string]string{"p": "# Twin\n"})
	os.Remove(pageFile(root, "p"))
	put(t, w, root, map[string]string{"q": "# Twin\n", "r": "# Twin\n"})
	if renames, _ := w.Renames(); len(renames) != 1 {
		t.Fatalf("two added copies made a rename: %v", renames)
	}

	for i := range maxRenames {
		if _, err := w.db.Exec(`INSERT INTO renames (old, new) VALUES (?, ?)`, fmt.Sprint("old", i), "new"); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(pageFile(root, "solo-moved"))
	put(t, w, root, map[string]string{"solo-again": "# Solo\n"})
	renames, err = w.Renames()
	if err != nil || len(renames) != maxRenames || renames[0] != [2]string{"solo-moved", "solo-again"} {
		t.Fatalf("%d renames, newest %v, %v", len(renames), renames[0], err)
	}
}

package wiki

import (
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

package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PutFile writes files that are not pages, refuses pages, hidden paths,
// escapes and symlinks, and replaces only when asked.
func TestPutFile(t *testing.T) {
	w, root := emptyWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)
	if err := w.PutFile("img/chart.png", []byte{0x89, 'P', 'N', 'G'}, false); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(pages, "img", "chart.png")); string(raw) != "\x89PNG" {
		t.Fatalf("chart.png = %q", raw)
	}
	if err := w.PutFile("img/chart.png", []byte("x"), false); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("overwrite without replace: %v", err)
	}
	if err := w.PutFile("img/chart.png", []byte("x"), true); err != nil {
		t.Errorf("replace: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(pages, "out")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"a.md", "b.qmd", "C.MD", "../x.png", "/etc/x", ".hidden/x.png", "out/x.png", ""} {
		if err := w.PutFile(rel, []byte("x"), true); err == nil {
			t.Errorf("PutFile(%q) wrote", rel)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("written through a symlink: %v", entries)
	}
}

// WritePageFile creates a page with the file's extension, replaces one that
// exists, and refuses the .qmd beside an .md.
func TestWritePageFile(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n"})
	if _, err := w.WritePageFile("notes/b.qmd", []byte("# B\n")); err != nil {
		t.Fatal(err)
	}
	if info, err := w.Find("notes/b"); err != nil || info.Ext != ".qmd" {
		t.Fatalf("notes/b = %+v, %v", info, err)
	}
	if _, err := w.WritePageFile("a.md", []byte("# A, theirs\n")); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "a"); got != "# A, theirs\n" {
		t.Fatalf("a = %q", got)
	}
	if _, err := w.WritePageFile("a.qmd", []byte("# Q\n")); err == nil || !strings.Contains(err.Error(), "is not the page a") {
		t.Errorf("a.qmd beside a.md: %v", err)
	}
	if _, err := w.WritePageFile("c.png", []byte("x")); err == nil {
		t.Error("wrote a non-page as a page")
	}
	if err := w.PutTemplate("standup.md", []byte("# Standup\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.PutTemplate("sub/x.md", []byte("x")); err == nil {
		t.Error("a template in a subdirectory was written")
	}
}

// Agents come from the per-user config only; a project's config naming one
// is an error.
func TestAgentsArePerUser(t *testing.T) {
	root := t.TempDir()
	p, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(t.TempDir(), "config.json")
	write(t, user, `{"agents": {"claude": {"command": "claude-agent-acp", "args": ["--x"]}}}`)
	if err := p.UseUserConfig(user); err != nil || p.Agents["claude"].Command != "claude-agent-acp" || p.Agents["claude"].Args[0] != "--x" {
		t.Fatalf("agents = %+v, %v", p.Agents, err)
	}
	write(t, user, `{"agents": {"claude": {"command": ""}}}`)
	if err := p.UseUserConfig(user); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Errorf("an agent without a command: %v", err)
	}
	write(t, filepath.Join(root, DirName, "config.json"), `{"name": "x", "agents": {"evil": {"command": "rm"}}}`)
	if _, err := Discover(root); err == nil || !strings.Contains(err.Error(), "per-user config.json only") {
		t.Errorf("project agents: %v", err)
	}
}

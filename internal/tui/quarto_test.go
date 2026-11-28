package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// :quarto render renders the open page, or the project selected in the tree,
// in the staged wiki, mirrors the output to .gwiki/render, and refuses unsaved
// edits, a directory without _quarto.yml, and a missing quarto.
func TestWikiQuartoRender(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in quarto is a /bin/sh script")
	}
	t.Setenv("TMPDIR", t.TempDir())
	f := newWikiFixture(t)
	f.m.focus = focusContent
	bin := t.TempDir()
	// A page render writes <stem>.html beside it; a project, _output/site.html.
	script := "#!/bin/sh\npwd > \"${0%/*}/dir\"\necho \"$@\" > \"${0%/*}/args\"\n" +
		"if [ -n \"$2\" ] && [ \"$2\" != --to ]; then echo x > \"${2%.*}.html\"; else mkdir -p _output; echo x > _output/site.html; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "quarto"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	render := filepath.Join(f.root, ".gwiki", "render")

	run := f.press(":quarto render to=pdf", "enter")
	if run == nil || !strings.Contains(f.m.status, "rendering index.md") {
		t.Fatalf("no render started: %q", f.m.status)
	}
	// An error from a command typed in the buffer shows on its message line.
	if f.press(":quarto render", "enter"); !strings.Contains(f.view(), "a render is running") {
		t.Fatalf("a second render:\n%s", f.view())
	}
	f.m.Update(run())
	if f.m.statusErr || f.m.status != "rendered index.html in .gwiki/render" || f.m.rendering != nil {
		t.Fatalf("status = %q", f.m.status)
	}
	if args, _ := os.ReadFile(filepath.Join(bin, "args")); string(args) != "render index.md --to pdf\n" {
		t.Fatalf("args = %q", args)
	}
	if cwd, _ := os.ReadFile(filepath.Join(bin, "dir")); strings.Contains(string(cwd), ".gwiki") {
		t.Fatalf("rendered under a hidden directory: %q", cwd)
	}
	if _, err := os.Stat(filepath.Join(render, "index.html")); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(f.root, ".gwiki", "wiki")); len(entries) != 4 {
		t.Fatalf("pages directory = %v", entries)
	}

	// A directory selected in the tree renders as a project.
	f.m.focus = focusTree
	for i, r := range f.m.rows {
		if r.dir == "lexer" {
			f.m.treeCursor = i
		}
	}
	if f.press(":quarto render", "enter"); !strings.Contains(f.m.status, "lexer has no _quarto.yml") {
		t.Fatalf("a directory without a project: %q", f.m.status)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".gwiki", "wiki", "lexer", "_quarto.yml"), []byte("project:\n  output-dir: _output\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.m.Update(f.press(":quarto render", "enter")())
	if f.m.status != "rendered lexer: 1 file in .gwiki/render" {
		t.Fatalf("project status = %q", f.m.status)
	}
	if args, _ := os.ReadFile(filepath.Join(bin, "args")); string(args) != "render\n" {
		t.Fatalf("project args = %q", args)
	}
	if _, err := os.Stat(filepath.Join(render, "lexer", "_output", "site.html")); err != nil {
		t.Fatal(err)
	}

	f.m.focus = focusContent
	f.press("i", "x", "esc")
	if f.press(":quarto render", "enter"); !strings.Contains(f.view(), "unsaved changes") {
		t.Fatalf("with unsaved edits:\n%s", f.view())
	}
	f.press("u")

	t.Setenv("PATH", t.TempDir())
	f.m.Update(f.press(":quarto render", "enter")())
	if !f.m.statusErr || !strings.Contains(f.m.status, "quarto is not in PATH") {
		t.Fatalf("without quarto: %q", f.m.status)
	}
	if f.press(":quarto preview", "enter"); !strings.Contains(f.view(), "usage: :quarto render") {
		t.Fatalf("bad arguments:\n%s", f.view())
	}
}

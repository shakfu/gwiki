package quarto

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/shakfu/gwiki/internal/wiki"
)

func TestParseArgs(t *testing.T) {
	for arg, want := range map[string]string{
		"render":           "",
		"render --to pdf":  "pdf",
		"render --to=docx": "docx",
		"render to=html":   "html",
	} {
		if got, err := ParseArgs(arg); err != nil || got != want {
			t.Errorf("ParseArgs(%q) = %q, %v; want %q", arg, got, err, want)
		}
	}
	for _, arg := range []string{"", "preview", "render pdf", "render --to", "render to=", "render --to -x", "render to=pdf extra"} {
		if _, err := ParseArgs(arg); err == nil {
			t.Errorf("ParseArgs(%q) accepted", arg)
		}
	}
}

// The staging directory is the user's own and has no hidden component.
func TestStageDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR is not read on Windows")
	}
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	dir, err := StageDir("/home/ada/lexer/.gwiki/wiki")
	if err != nil || !strings.HasPrefix(filepath.Base(dir), "lexer-") || hidden(dir) {
		t.Fatalf("StageDir = %q, %v", dir, err)
	}
	if info, err := os.Stat(filepath.Dir(dir)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("user directory: %v, %v", info, err)
	}
	if again, err := StageDir("/home/ada/lexer/.gwiki/wiki"); err != nil || again != dir {
		t.Fatalf("StageDir again = %q, %v", again, err)
	}
	if other, _ := StageDir("/home/ada/other/.gwiki/wiki"); other == dir {
		t.Fatal("two wikis share a staging directory")
	}
	if dot, err := StageDir("/home/ada/.dotfiles/.gwiki/wiki"); err != nil || !strings.HasPrefix(filepath.Base(dot), "dotfiles-") {
		t.Fatalf("a wiki in a dot directory: %q, %v", dot, err)
	}

	if err := os.Chmod(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := StageDir("/home/ada/lexer/.gwiki/wiki"); err == nil {
		t.Fatal("accepted a directory others can enter")
	}

	t.Setenv("TMPDIR", filepath.Join(tmp, ".hidden"))
	if err := os.Mkdir(filepath.Join(tmp, ".hidden"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := StageDir("/home/ada/lexer/.gwiki/wiki"); err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("StageDir under a hidden directory = %v", err)
	}
}

// fakeQuarto puts a quarto first in PATH that records its arguments and
// directory, then runs body.
func fakeQuarto(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in quarto is a /bin/sh script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\npwd > \"${0%/*}/dir\"\necho \"$@\" > \"${0%/*}/args\"\n" + body
	if err := os.WriteFile(filepath.Join(bin, "quarto"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func write(t *testing.T, file, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A page renders in its staged directory; what quarto wrote is mirrored to
// Out, and what it deleted is removed there, but not the inputs or caches.
func TestRenderMirrorsOutputs(t *testing.T) {
	bin := fakeQuarto(t, "echo html > a.html\nmkdir -p a_files .quarto ../_freeze\necho js > a_files/lib.js\necho c > .quarto/cache\necho f > ../_freeze/x\nrm -f stale.html\n")
	stage, out := t.TempDir(), filepath.Join(t.TempDir(), "render")
	write(t, filepath.Join(stage, "sub", "a.qmd"), "# A\n")
	write(t, filepath.Join(stage, "sub", "stale.html"), "old\n")
	write(t, filepath.Join(out, "sub", "stale.html"), "old\n")

	job := Job{Stage: stage, Inputs: map[string]bool{"sub/a.qmd": true}, File: "sub/a.qmd", Format: "pdf", Out: out}
	res, err := Render(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if args, _ := os.ReadFile(filepath.Join(bin, "args")); string(args) != "render a.qmd --to pdf\n" {
		t.Errorf("args = %q", args)
	}
	if cwd, _ := os.ReadFile(filepath.Join(bin, "dir")); filepath.Base(strings.TrimSpace(string(cwd))) != "sub" {
		t.Errorf("ran in %q", cwd)
	}
	if !slices.Equal(res.Written, []string{"sub/a.html", "sub/a_files/lib.js"}) || !slices.Equal(res.Removed, []string{"sub/stale.html"}) {
		t.Fatalf("Written = %v, Removed = %v", res.Written, res.Removed)
	}
	if got, err := os.ReadFile(filepath.Join(out, "sub", "a_files", "lib.js")); err != nil || string(got) != "js\n" {
		t.Errorf("mirrored lib.js = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(out, "sub", "stale.html")); !os.IsNotExist(err) {
		t.Errorf("stale.html was kept: %v", err)
	}
	for _, rel := range []string{"sub/a.qmd", "sub/.quarto/cache", "_freeze/x"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s was mirrored", rel)
		}
	}
	if ignore, err := os.ReadFile(filepath.Join(out, ".gitignore")); err != nil || string(ignore) != "*\n" {
		t.Errorf(".gitignore = %q, %v", ignore, err)
	}

	// A project renders from its own directory, with no file named.
	if _, err := Render(context.Background(), Job{Stage: stage, Dir: "sub", Out: out}); err != nil {
		t.Fatal(err)
	}
	if args, _ := os.ReadFile(filepath.Join(bin, "args")); string(args) != "render\n" {
		t.Errorf("project args = %q", args)
	}
}

func TestRenderFailureKeepsTheLog(t *testing.T) {
	fakeQuarto(t, "echo 'starting'\necho 'ERROR: no such format' >&2\nexit 1\n")
	stage, out := t.TempDir(), filepath.Join(t.TempDir(), "render")
	_, err := Render(context.Background(), Job{Stage: stage, File: "a.md", Format: "nope", Out: out})
	if err == nil || !strings.Contains(err.Error(), "ERROR: no such format") {
		t.Fatalf("err = %v", err)
	}
	log, rerr := os.ReadFile(filepath.Join(out, "a.md.log"))
	if rerr != nil || !strings.Contains(string(log), "starting") {
		t.Fatalf("log = %q, %v", log, rerr)
	}
}

func TestRenderWithoutQuarto(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Render(context.Background(), Job{Stage: t.TempDir(), File: "a.qmd", Out: t.TempDir()}); !errors.Is(err, ErrMissing) {
		t.Fatalf("err = %v", err)
	}
}

// With quarto installed, a page in a project under .gwiki/wiki takes the
// project's settings and output-dir, a project renders every page and turns
// [[wiki]] links into links, and a page listing two formats renders both.
func TestRenderWithQuarto(t *testing.T) {
	if _, err := exec.LookPath("quarto"); err != nil {
		t.Skip("quarto is not installed")
	}
	if testing.Short() {
		t.Skip("quarto takes seconds to start")
	}
	if runtime.GOOS != "windows" {
		t.Setenv("TMPDIR", t.TempDir())
	}
	root := t.TempDir()
	p, err := wiki.Init(root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wiki.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pages := w.PagesPath()
	write(t, filepath.Join(pages, "book", Project), "project:\n  type: website\n  output-dir: _output\ntoc: true\n")
	write(t, filepath.Join(pages, "book", "ch1.qmd"), "---\ntitle: One\n---\n\nSee [[Two]].\n\n## Part\n")
	write(t, filepath.Join(pages, "book", "ch2.qmd"), "---\ntitle: Two\n---\n\n## Part\n")
	write(t, filepath.Join(pages, "doc.qmd"), "---\ntitle: Doc\nformat:\n  html: default\n  docx: default\n---\n\n# Hi\n")

	stage, err := StageDir(pages)
	if err != nil {
		t.Fatal(err)
	}
	render := func(job Job) Result {
		t.Helper()
		exp, err := w.Export(stage)
		if err != nil {
			t.Fatal(err)
		}
		job.Stage, job.Out, job.Inputs = stage, w.RenderPath(), map[string]bool{}
		for _, rel := range exp.Written {
			job.Inputs[rel] = true
		}
		res, err := Render(context.Background(), job)
		if err != nil {
			t.Fatalf("%+v: %v\n%s", job, err, res.Log)
		}
		return res
	}
	has := func(res Result, rel string) {
		t.Helper()
		if !slices.Contains(res.Written, rel) {
			t.Errorf("%s not written: %v", rel, res.Written)
		}
	}

	res := render(Job{File: "book/ch1.qmd"})
	has(res, "book/_output/ch1.html")
	ch1 := filepath.Join(w.RenderPath(), "book", "_output", "ch1.html")
	if html, _ := os.ReadFile(ch1); !strings.Contains(string(html), `id="TOC"`) {
		t.Error("ch1.html lacks the project's toc")
	}

	// A website project points the link at ch2.html; a default one keeps
	// ch2.qmd, which quarto does not rewrite.
	res = render(Job{Dir: "book"})
	has(res, "book/_output/ch1.html")
	has(res, "book/_output/ch2.html")
	if html, _ := os.ReadFile(ch1); !strings.Contains(string(html), `href="./ch2.html"`) {
		t.Error("[[Two]] is not a link to ch2.html")
	}

	res = render(Job{File: "doc.qmd"})
	has(res, "doc.html")
	has(res, "doc.docx")
	if entries, _ := os.ReadDir(pages); len(entries) != 2 {
		t.Errorf("left in the pages directory: %v", entries)
	}
}

package wiki

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func templateWiki(t *testing.T) (*Wiki, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saved := today
	today = func() string { return "2026-11-28" }
	t.Cleanup(func() { today = saved })
	return emptyWiki(t)
}

func TestBuiltinTemplates(t *testing.T) {
	w, _ := templateWiki(t)
	all, err := w.Templates()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tp := range all {
		names = append(names, tp.Name+tp.Ext)
		if tp.Origin != "builtin" || tp.File != "" {
			t.Errorf("%+v", tp)
		}
	}
	want := []string{"analysis.qmd", "article.qmd", "daily.md", "decision.md", "design.md", "howto.md", "meeting.md", "postmortem.md", "report.qmd", "review.md", "slides.qmd"}
	if !slices.Equal(names, want) {
		t.Fatalf("Templates = %v", names)
	}
	// Each makes a page that parses, titled and indexed as given.
	for _, tp := range all {
		info, _, err := w.Create(NewPage{Title: "From " + tp.Name, Template: tp.Name})
		if err != nil || info.Title != "From "+tp.Name || info.Ext != tp.Ext {
			t.Errorf("%s: %+v, %v", tp.Name, info, err)
		}
	}
}

// A heading template is titled in its heading and dated today; tags merge;
// a Quarto template is titled in its front matter and keeps its comments.
func TestCreateFromTemplate(t *testing.T) {
	w, root := templateWiki(t)
	pages := filepath.Join(root, DirName, PagesDir)

	info, _, err := w.Create(NewPage{Title: "Sprint review", Template: "meeting", Tags: []string{"team"}, Body: "Extra."})
	if err != nil || info.Path != "sprint-review" || info.Ext != ".md" || !slices.Equal(info.Tags, []string{"meeting", "team"}) {
		t.Fatalf("Create = %+v, %v", info, err)
	}
	src := source(t, root, "sprint-review")
	if !strings.HasPrefix(src, "---\ndate: 2026-11-28\ntags: [meeting, team]\n---\n\n# Sprint review\n") || !strings.HasSuffix(src, "- [ ] owner: action\n\nExtra.\n") {
		t.Fatalf("meeting page:\n%s", src)
	}

	if _, _, err := w.Create(NewPage{Title: "Q3 sales", Template: "report"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(pages, "q3-sales.qmd"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"title: Q3 sales\n", "date: 2026-11-28\n", "# pdf needs a LaTeX install", "#  pdf:", "## Summary"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("report page lacks %q:\n%s", want, raw)
		}
	}
}

// A project template replaces a built-in one, a per-user one fills in, quarto's
// date: today is kept, and a template with neither title key nor heading gets
// a title key.
func TestProjectAndUserTemplates(t *testing.T) {
	w, root := templateWiki(t)
	project := filepath.Join(root, DirName, TemplatesDir)
	user, err := UserTemplatesDir()
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(project, "meeting.md"), "# Ours\n\nAgenda.\n")
	write(t, filepath.Join(user, "talk.qmd"), "---\ndate: today\nformat: revealjs\n---\n\n## One\n")
	write(t, filepath.Join(user, "meeting.md"), "# Theirs\n")

	all, err := w.Templates()
	if err != nil {
		t.Fatal(err)
	}
	origin := map[string]string{}
	for _, tp := range all {
		origin[tp.Name] = tp.Origin
	}
	if origin["meeting"] != "project" || origin["talk"] != "user" || origin["review"] != "builtin" {
		t.Fatalf("origins = %v", origin)
	}

	if _, _, err := w.Create(NewPage{Title: "Standup", Template: "meeting"}); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "standup"); got != "# Standup\n\nAgenda.\n" {
		t.Fatalf("standup = %q", got)
	}
	if _, _, err := w.Create(NewPage{Title: "Launch", Template: "talk"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, DirName, PagesDir, "launch.qmd"))
	if string(raw) != "---\ndate: today\nformat: revealjs\ntitle: Launch\n---\n\n## One\n" {
		t.Fatalf("launch.qmd = %q", raw)
	}

	if _, _, err := w.Create(NewPage{Title: "X", Template: "nope"}); err == nil || !strings.Contains(err.Error(), "there are analysis, article") {
		t.Errorf("unknown template: %v", err)
	}
	if _, _, err := w.Create(NewPage{Title: "X", Template: "../meeting"}); err == nil || !strings.Contains(err.Error(), "not a template name") {
		t.Errorf("a path as a template: %v", err)
	}
	write(t, filepath.Join(project, "meeting.qmd"), "# Q\n")
	if _, err := w.Templates(); err == nil || !strings.Contains(err.Error(), "remove one") {
		t.Errorf("meeting.md and meeting.qmd: %v", err)
	}
}

// A template that is a symlink is skipped, so a cloned repository cannot copy
// a file of the reader's into a page; the other templates still work, and the
// skipped one hides the built-in one of its name.
func TestSymlinkedTemplateIsSkipped(t *testing.T) {
	w, root := templateWiki(t)
	secret := filepath.Join(t.TempDir(), "credentials")
	write(t, secret, "key = hunter2\n")
	project := filepath.Join(root, DirName, TemplatesDir)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(project, "meeting.md")); err != nil {
		t.Skip("symlinks are not available:", err)
	}
	all, err := w.Templates()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(all, func(tp Template) bool { return tp.Name == "meeting" })
	if i < 0 || all[i].Origin != "project" || !strings.Contains(all[i].Skipped, "symlink") {
		t.Fatalf("meeting = %+v", all)
	}
	if _, _, err := w.Create(NewPage{Title: "Standup", Template: "meeting"}); err == nil || !strings.Contains(err.Error(), "is skipped") {
		t.Fatalf("Create = %v", err)
	}
	if _, err := os.Stat(pageFile(root, "standup")); !os.IsNotExist(err) {
		t.Fatal("a page was written from the symlink")
	}
	if _, _, err := w.Create(NewPage{Title: "Retro", Template: "review"}); err != nil {
		t.Fatalf("another template: %v", err)
	}
	if _, _, err := w.Create(NewPage{Title: "X", Template: "nope"}); err == nil || strings.Contains(err.Error(), "meeting") {
		t.Errorf("the skipped template is offered: %v", err)
	}
}

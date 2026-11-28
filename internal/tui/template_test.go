package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// :new with template= starts the page from a template and takes its
// extension; without a title it asks for one and keeps the template.
func TestWikiNewFromTemplate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newWikiFixture(t)
	f.m.focus = focusContent
	f.press(":new Sales report template=report", "enter")
	if f.page() != "sales-report" || f.m.cur.info.Ext != ".qmd" {
		t.Fatalf("opened %q (%+v)\n%s", f.page(), f.m.cur, f.view())
	}
	if _, err := os.Stat(filepath.Join(f.root, ".gwiki", "wiki", "sales-report.qmd")); err != nil {
		t.Fatal(err)
	}
	f.press(":new template=meeting", "enter", "Standup", "enter")
	if f.page() != "standup" || !strings.Contains(f.source("standup"), "## Agenda") {
		t.Fatalf("opened %q", f.page())
	}
	if f.press(":new X template=nope", "enter"); !strings.Contains(f.view(), `no template "nope"`) {
		t.Fatalf("unknown template:\n%s", f.view())
	}
}

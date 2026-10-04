package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// On a terminal, colour is on and table cells print their styled form, which
// must be as safe as the plain form.
func TestWikiColorDoesNotPrintControlCharacters(t *testing.T) {
	f := wikiFixture(t)
	evil := "---\ntitle: Evil\n---\n\nSee [[Gone\x1b]0;PWNED\x07 page]].\n\n- [ ] pay\x1b]0;PWNED\x07 up\n"
	if err := os.WriteFile(filepath.Join(f.dir, ".gwiki", "wiki", "evil.md"), []byte(evil), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"links", "evil"}, {"show", "evil"}, {"check"}, {"tasks"}} {
		var out, errBuf bytes.Buffer
		app := &App{Stdout: &out, Stderr: &errBuf, Stdin: strings.NewReader(""), Dir: f.dir, Now: func() time.Time { return f.now }, Color: true}
		app.Run(args)
		if s := out.String(); strings.Contains(s, "\x1b]") || strings.Contains(s, "\x07") {
			t.Errorf("gwiki %s printed a control sequence: %q", strings.Join(args, " "), s)
		}
		if !strings.Contains(out.String(), "PWNED") {
			t.Errorf("gwiki %s did not print the page text: %q %q", strings.Join(args, " "), out.String(), errBuf.String())
		}
	}
}

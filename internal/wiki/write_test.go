package wiki

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// emptyWiki is a repository with an initialised wiki and no pages.
func emptyWiki(t *testing.T) (*Wiki, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, root
}

func pageFile(root, id string) string {
	return filepath.Join(root, DirName, PagesDir, filepath.FromSlash(id)+".md")
}

func put(t *testing.T, w *Wiki, root string, pages map[string]string) {
	t.Helper()
	for id, body := range pages {
		write(t, pageFile(root, id), body)
	}
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
}

func source(t *testing.T, root, id string) string {
	t.Helper()
	raw, err := os.ReadFile(pageFile(root, id))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAWriteOverAChangedPageIsAConflict(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n", "b": "# B\n"})

	_, hashA, _ := w.Read("a")
	_, hashB, _ := w.Read("b")
	// Another writer, such as an editor or an agent, saves first.
	write(t, pageFile(root, "b"), "# B, edited elsewhere\n")

	_, err := w.commit([]fileWrite{{Page: "a", Base: hashA, Data: []byte("# A, mine\n")}, {Page: "b", Base: hashB, Data: []byte("# B, mine\n")}})
	var conflict *ErrConflict
	if !errors.As(err, &conflict) || conflict.Page != "b" || string(conflict.Current) != "# B, edited elsewhere\n" {
		t.Fatalf("commit = %v, want a conflict on b carrying its current content", err)
	}
	if got := source(t, root, "a"); got != "# A\n" {
		t.Fatalf("a = %q: part of a refused batch was written", got)
	}
	if got := source(t, root, "b"); got != "# B, edited elsewhere\n" {
		t.Fatalf("b = %q: the other writer's save was overwritten", got)
	}

	// Two gwiki processes: the second writes against what it read before the
	// first wrote.
	other, err := Open(w.Project)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, base, _ := other.Read("a")
	if _, err := w.Write("a", []byte("# A, first\n"), base); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Write("a", []byte("# A, second\n"), base); !errors.As(err, &conflict) {
		t.Fatalf("second write = %v, want a conflict", err)
	}
	if _, err := w.commit([]fileWrite{{Page: "a", Data: []byte("x")}}); !errors.Is(err, ErrExists) {
		t.Fatalf("creating over a page = %v, want ErrExists", err)
	}
}

func TestCreateSetBodyAndTags(t *testing.T) {
	w, root := emptyWiki(t)
	info, _, err := w.Create(NewPage{Title: "Fix the lexer!", Dir: "tasks", Task: true, Tags: []string{"Bug", "parser"}, Body: "Body text."})
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntype: task\nstatus: open\ntags: [bug, parser]\n---\n\n# Fix the lexer!\n\nBody text.\n"
	if info.Path != "tasks/fix-the-lexer" || source(t, root, info.Path) != want {
		t.Fatalf("created %s:\n%q\nwant:\n%q", info.Path, source(t, root, info.Path), want)
	}
	if again, _, _ := w.Create(NewPage{Title: "Fix the lexer", Dir: "tasks"}); again.Path != "tasks/fix-the-lexer-2" {
		t.Fatalf("second page got %q, want a suffix", again.Path)
	}
	if _, _, err := w.Create(NewPage{Title: "x", Dir: "../out"}); err == nil {
		t.Fatal("a page outside the wiki was created")
	}

	if _, err := w.SetBody(info.Path, "# Fix the lexer!\n\nNew body."); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, info.Path); !strings.HasPrefix(got, "---\ntype: task") || !strings.HasSuffix(got, "New body.\n") {
		t.Fatalf("SetBody lost the front matter:\n%s", got)
	}

	write(t, pageFile(root, "notes"), "---\n# kept comment\ntitle: Notes\ntags:\n  - one\n---\n\nText.\n")
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tag("notes", []string{"#Two", "one"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "notes"); got != "---\n# kept comment\ntitle: Notes\ntags:\n  - one\n  - two\n---\n\nText.\n" {
		t.Fatalf("after tagging:\n%q", got)
	}
	if _, err := w.Tag("notes", nil, []string{"ONE", "two"}); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "notes"); got != "---\n# kept comment\ntitle: Notes\n---\n\nText.\n" {
		t.Fatalf("after untagging:\n%q", got)
	}
	write(t, pageFile(root, "bare"), "# Bare\n")
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tag("bare", []string{"new"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "bare"); got != "---\ntags: [new]\n---\n\n# Bare\n" {
		t.Fatalf("tagging a page without front matter:\n%q", got)
	}
	if page, _ := w.Page("bare"); strings.Join(page.Tags, ",") != "new" {
		t.Fatalf("the cache did not follow the tag: %v", page.Tags)
	}
}

func TestTasksChangeStatusAndPromote(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"plan":       "# Plan\n\n- [ ] write the grammar due:2026-09-01\n- [x] sketch the lexer\n- [ ] benchmark it\n",
		"tasks/ship": "---\ntitle: Ship\ntype: task\nstatus: open\n---\n",
	})

	item, err := w.FindTask("grammar")
	if err != nil || item.Page != "plan" || item.Line != 3 {
		t.Fatalf("FindTask(grammar) = %+v, %v", item, err)
	}
	if _, err := w.SetTaskStatus(item, "done"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source(t, root, "plan"), "- [x] write the grammar") {
		t.Fatalf("item not ticked:\n%s", source(t, root, "plan"))
	}
	if _, err := w.SetTaskStatus(item, "doing"); err == nil {
		t.Fatal("a checklist item took doing")
	}
	if byLine, err := w.FindTask("plan:4"); err != nil || byLine.Text != "sketch the lexer" {
		t.Fatalf("FindTask(plan:4) = %+v, %v", byLine, err)
	}
	if _, err := w.FindTask("the"); err == nil || !strings.Contains(err.Error(), "could mean") {
		t.Fatalf("an ambiguous fragment = %v", err)
	}

	page, err := w.FindTask("ship")
	if err != nil || page.Line != 0 {
		t.Fatalf("FindTask(ship) = %+v, %v", page, err)
	}
	if _, err := w.SetTaskStatus(page, "doing"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source(t, root, "tasks/ship"), "status: doing") {
		t.Fatalf("task page status:\n%s", source(t, root, "tasks/ship"))
	}

	item, _ = w.FindTask("write the grammar")
	created, _, err := w.Promote(item, "tasks")
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != "tasks/write-the-grammar" || created.Status != "done" || created.Due != "2026-09-01" {
		t.Fatalf("promoted page = %+v", created)
	}
	if got := source(t, root, "plan"); !strings.Contains(got, "- [[write the grammar due:2026-09-01]]") && !strings.Contains(got, "- [[write the grammar]]") {
		t.Fatalf("item not replaced by a link:\n%s", got)
	}
	if links, _ := w.Links("plan"); len(links) != 1 || links[0].Status != StatusOK || links[0].Resolved != created.Path {
		t.Fatalf("link to the promoted page = %+v", links)
	}
	if tasks, _ := w.Tasks(TaskFilter{Page: "plan"}); len(tasks) != 2 {
		t.Fatalf("plan still lists %d items, want 2", len(tasks))
	}
}

// An item's box offset comes from the cache; a page changed since indexing is
// refused rather than ticked at a stale offset.
func TestTaskEditsRefuseAPageChangedSinceIndexing(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"plan": "# Plan\n\n- [ ] first\n- [ ] second\n"})
	item, err := w.FindTask("plan:4")
	if err != nil {
		t.Fatal(err)
	}
	changed := "# Plan\n\n- [ ] new\n- [ ] first\n- [ ] second\n"
	write(t, pageFile(root, "plan"), changed)

	var conflict *ErrConflict
	if _, err := w.SetTaskStatus(item, "done"); !errors.As(err, &conflict) {
		t.Fatalf("SetTaskStatus = %v, want a conflict", err)
	}
	if _, _, err := w.Promote(item, "tasks"); !errors.As(err, &conflict) {
		t.Fatalf("Promote = %v, want a conflict", err)
	}
	if got := source(t, root, "plan"); got != changed {
		t.Fatalf("plan was written:\n%s", got)
	}
}

// A Task held across a refresh can point at another item (W5) or past the end
// of a shortened page (W4).
func TestTaskEditsRefuseAHeldItemThatMoved(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"plan": "# Plan\n\n- [ ] alpha\n- [ ] bravo\n"})
	alpha, err := w.FindTask("alpha")
	if err != nil {
		t.Fatal(err)
	}

	var conflict *ErrConflict
	swapped := "# Plan\n\n- [ ] bravo\n- [ ] alpha\n"
	put(t, w, root, map[string]string{"plan": swapped})
	if _, err := w.SetTaskStatus(alpha, "done"); !errors.As(err, &conflict) {
		t.Fatalf("SetTaskStatus on a swapped item = %v, want a conflict", err)
	}
	if _, _, err := w.Promote(alpha, "tasks"); !errors.As(err, &conflict) {
		t.Fatalf("Promote on a swapped item = %v, want a conflict", err)
	}
	if got := source(t, root, "plan"); got != swapped {
		t.Fatalf("plan was written:\n%s", got)
	}

	put(t, w, root, map[string]string{"plan": "# P\n"})
	if _, err := w.SetTaskStatus(alpha, "done"); !errors.As(err, &conflict) {
		t.Fatalf("SetTaskStatus past the end = %v, want a conflict", err)
	}
	if _, _, err := w.Promote(alpha, "tasks"); !errors.As(err, &conflict) {
		t.Fatalf("Promote past the end = %v, want a conflict", err)
	}
}

func TestReplaceOneSpan(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n\naaa and b and b\n"})
	_, hash, _ := w.Read("a")

	for old, want := range map[string]string{
		"":     "empty",
		"zzz":  "does not contain",
		"b":    "occurs 2 times",
		"aa":   "occurs 2 times", // overlapping
		"# A!": "does not contain",
	} {
		if _, _, err := w.Replace("a", old, "x", hash); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Replace(%q) = %v, want %q", old, err, want)
		}
	}

	next, _, err := w.Replace("a", "and b\n", "and c\n", hash)
	if err != nil {
		t.Fatal(err)
	}
	if got := source(t, root, "a"); got != "# A\n\naaa and b and c\n" || next != Hash([]byte(got)) {
		t.Fatalf("after Replace: %q, hash %s", got, next)
	}

	// The first hash is stale now; the conflict carries the current page.
	var conflict *ErrConflict
	if _, _, err := w.Replace("a", "aaa", "x", hash); !errors.As(err, &conflict) || conflict.CurrentHash != next {
		t.Fatalf("Replace with a stale base = %v", err)
	}
}

func TestRemoveRefusesWhileLinked(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"target": "# Target\n", "source": "See [[Target]].\n"})
	back, _, err := w.Remove("target", false)
	if err == nil || len(back) != 1 {
		t.Fatalf("Remove = %v, %v; want a refusal naming the link", back, err)
	}
	if _, _, err := w.Remove("target", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pageFile(root, "target")); !os.IsNotExist(err) {
		t.Fatal("the page is still there")
	}
	if links, _ := w.Links("source"); links[0].Status != StatusMissingPage {
		t.Fatalf("link after removal = %+v", links[0])
	}
}

// moveFixture has links to lexer/design-sketch in both forms, from pages in
// several directories, with labels, anchors and reference definitions, and
// links from the page to its neighbours.
func moveFixture(t *testing.T) (*Wiki, string) {
	t.Helper()
	w, root := emptyWiki(t)
	write(t, filepath.Join(root, "src", "lexer.go"), "package lexer\n\nfunc Lex() {}\n")
	put(t, w, root, map[string]string{
		"lexer/design-sketch": "# Design sketch\n\n## Tokens\n\n" +
			"[grammar](grammar.md#rules) [code](../../../src/lexer.go#L2) [self](design-sketch.md#tokens) [[index]] [ref][r]\n\n" +
			"[r]: grammar.md\n",
		"lexer/grammar": "# Grammar\n\n## Rules\n\nSee [[design-sketch]].\n",
		"index": "# Index\n\n" +
			"- [[Design sketch]]\n" +
			"- [[lexer/design-sketch]]\n" +
			"- [[lexer/design-sketch#Tokens|the tokens]]\n" +
			"- [md](lexer/design-sketch.md#tokens)\n" +
			"- [rooted](</.gwiki/wiki/lexer/design-sketch.md>)\n" +
			"- [by reference][d]\n\n" +
			"[d]: lexer/design-sketch.md\n",
		"notes/deep/other": "# Other\n\n[up](../../lexer/design-sketch.md) and [[sketch]]\n",
		"elsewhere/sketch": "# Unrelated sketch\n",
		"notes/far":        "# Far\n\n[[sketch]] and [[nowhere]]\n",
	})
	return w, root
}

func TestMoveRewritesLinksInTheirOwnForm(t *testing.T) {
	w, root := moveFixture(t)

	plan, err := w.PlanMove("lexer/design-sketch", "archive/old/sketch")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) == 0 {
		t.Fatal("the plan has no edits")
	}
	if _, err := os.Stat(pageFile(root, "archive/old/sketch")); !os.IsNotExist(err) {
		t.Fatal("planning wrote the moved page")
	}

	res, _, err := w.Move("lexer/design-sketch", "archive/old/sketch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pageFile(root, "lexer/design-sketch")); !os.IsNotExist(err) {
		t.Fatal("the old file is still there")
	}

	for id, want := range map[string]string{
		"index": "# Index\n\n" +
			"- [[Design sketch]]\n" + // by title: still resolves, untouched
			"- [[archive/old/sketch|lexer/design-sketch]]\n" + // file name sketch is ambiguous, so the path; old text kept as label
			"- [[archive/old/sketch#Tokens|the tokens]]\n" +
			"- [md](archive/old/sketch.md#tokens)\n" +
			"- [rooted](</.gwiki/wiki/archive/old/sketch.md>)\n" +
			"- [by reference][d]\n\n" +
			"[d]: archive/old/sketch.md\n",
		"lexer/grammar":    "# Grammar\n\n## Rules\n\nSee [[archive/old/sketch|design-sketch]].\n",
		"notes/deep/other": "# Other\n\n[up](../../archive/old/sketch.md) and [[sketch]]\n",
		"archive/old/sketch": "# Design sketch\n\n## Tokens\n\n" +
			"[grammar](../../lexer/grammar.md#rules) [code](../../../../src/lexer.go#L2) [self](sketch.md#tokens) [[index]] [ref][r]\n\n" +
			"[r]: ../../lexer/grammar.md\n",
	} {
		if got := source(t, root, id); got != want {
			t.Errorf("%s after the move:\n%s\nwant:\n%s", id, got, want)
		}
	}

	for _, id := range []string{"index", "lexer/grammar", "archive/old/sketch"} {
		links, _ := w.Links(id)
		for _, l := range links {
			if l.Status != StatusOK {
				t.Errorf("%s: %s is %s", id, l.Written(), l.Status)
			}
		}
	}
	// [[sketch]] meant elsewhere/sketch by file name; now two pages have it.
	// notes/far is not edited by the move; its [[nowhere]] was already broken.
	var broken []string
	for _, l := range res.Broken {
		broken = append(broken, l.Page+" "+l.Written()+" "+l.Status)
	}
	if got := strings.Join(broken, ", "); got != "notes/deep/other [[sketch]] ambiguous, notes/far [[sketch]] ambiguous" {
		t.Fatalf("Broken = %s, want the newly ambiguous [[sketch]] links", got)
	}
}

// An untitled page is titled by its file name; a move retitles it, so a link
// by the old title is rewritten, and planning reports it.
func TestMoveRewritesLinksToAnUntitledPage(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"foo": "No heading here.\n",
		"a":   "# A\n\nSee [[foo]] and [[Foo|the foo]].\n",
	})
	plan, err := w.PlanMove("foo", "sub/bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 2 {
		t.Fatalf("plan edits = %+v, want both links", plan.Edits)
	}
	if _, _, err := w.Move("foo", "sub/bar"); err != nil {
		t.Fatal(err)
	}
	links, _ := w.Links("a")
	for _, l := range links {
		if l.Status != StatusOK || l.Resolved != "sub/bar" {
			t.Errorf("%s is %s, resolved to %q\n%s", l.Written(), l.Status, l.Resolved, source(t, root, "a"))
		}
	}
}

// A page's wiki links to itself are rewritten with it, and planning reports
// them under the new path.
func TestMoveRewritesAPagesLinksToItself(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"old-name": "# Notes\n\n## Sec\n\nSee [[old-name#Sec]] and [[Notes]].\n",
	})
	plan, err := w.PlanMove("old-name", "new-name")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Edits) != 1 || plan.Edits[0].Page != "new-name" || plan.Edits[0].Old != "old-name#Sec" {
		t.Fatalf("plan edits = %+v, want the one self link by name", plan.Edits)
	}
	if _, _, err := w.Move("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	links, _ := w.Links("new-name")
	for _, l := range links {
		if l.Status != StatusOK || l.Resolved != "new-name" {
			t.Errorf("%s is %s\n%s", l.Written(), l.Status, source(t, root, "new-name"))
		}
	}
}

// A page edited between planning and writing refuses the whole move.
func TestMoveRefusesWhenALinkingPageChanged(t *testing.T) {
	w, root := moveFixture(t)
	plan, err := w.PlanMove("lexer/design-sketch", "archive/sketch")
	if err != nil {
		t.Fatal(err)
	}
	before := source(t, root, "lexer/design-sketch")
	write(t, pageFile(root, "lexer/grammar"), "# Grammar\n\n## Rules\n\nEdited elsewhere. See [[design-sketch]].\n")

	var conflict *ErrConflict
	if _, err := w.commit(plan.writes); !errors.As(err, &conflict) || conflict.Page != "lexer/grammar" {
		t.Fatalf("commit = %v, want a conflict on lexer/grammar", err)
	}
	if _, err := os.Stat(pageFile(root, "archive/sketch")); !os.IsNotExist(err) {
		t.Fatal("the refused move wrote the new page")
	}
	if source(t, root, "lexer/design-sketch") != before || !strings.Contains(source(t, root, "index"), "[[lexer/design-sketch]]") {
		t.Fatal("the refused move changed a page")
	}
	if _, _, err := w.Move("lexer/design-sketch", "index"); !errors.Is(err, ErrExists) {
		t.Fatalf("moving onto a page = %v, want ErrExists", err)
	}
}

func TestOffersAndFixes(t *testing.T) {
	w, root := emptyWiki(t)
	write(t, filepath.Join(root, "src", "lexer.go"), "package lexer\n")
	put(t, w, root, map[string]string{
		"lexer/design-sketch": "# Design sketch\n\n## Tokens\n",
		"a/dup":               "# Dup\n",
		"b/dup":               "# Dup\n",
		"notes/moved":         "# Moved away\n\nUnique text.\n",
		"index": "# Index\n\n" +
			"- [[Desing sketch]]\n" +
			"- [[Dup]]\n" +
			"- [[Design sketch#Tokns]]\n" +
			"- [code](../old/lexer.go)\n" +
			"- [line](../../src/lexer.go#L9)\n" +
			"- [gone](notes/moved.md)\n",
	})
	// notes/moved is renamed outside gwiki.
	raw := source(t, root, "notes/moved")
	if err := os.Remove(pageFile(root, "notes/moved")); err != nil {
		t.Fatal(err)
	}
	write(t, pageFile(root, "archive/moved"), raw)
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}

	broken, err := w.Check()
	if err != nil || len(broken) != 6 {
		t.Fatalf("broken = %d, %v", len(broken), err)
	}
	want := map[string]string{
		"[[Desing sketch]]":       "Design sketch",
		"[[Dup]]":                 "a/dup",
		"[[Design sketch#Tokns]]": "Design sketch#Tokens",
		"../old/lexer.go":         "../../src/lexer.go",
		"../../src/lexer.go#L9":   "../../src/lexer.go",
		"notes/moved.md":          "archive/moved.md",
	}
	for _, l := range broken {
		offers, err := w.Offers(l)
		if err != nil {
			t.Fatalf("%s: %v", l.Written(), err)
		}
		if len(offers) == 0 || offers[0].New != want[l.Written()] {
			t.Errorf("%s (%s): offers %+v, want first %q", l.Written(), l.Status, offers, want[l.Written()])
		}
	}

	// Fixing one link at a time, re-reading the broken list, as check --fix does.
	for {
		broken, err := w.Check()
		if err != nil {
			t.Fatal(err)
		}
		if len(broken) == 0 {
			break
		}
		offers, _ := w.Offers(broken[0])
		if len(offers) == 0 {
			t.Fatalf("no offer for %s", broken[0].Written())
		}
		if _, err := w.Fix(broken[0], offers[0]); err != nil {
			t.Fatalf("Fix %s: %v", broken[0].Written(), err)
		}
	}
	got := source(t, root, "index")
	for _, s := range []string{"[[Design sketch|Desing sketch]]", "[[a/dup|Dup]]", "[[Design sketch#Tokens|Design sketch#Tokns]]", "[code](../../src/lexer.go)", "[line](../../src/lexer.go)", "(archive/moved.md)"} {
		if !strings.Contains(got, s) {
			t.Errorf("index after fixing is missing %q:\n%s", s, got)
		}
	}
}

func TestBoundedDistanceMatchesLevenshtein(t *testing.T) {
	naive := func(a, b string) int {
		ra, rb := []rune(a), []rune(b)
		d := make([][]int, len(ra)+1)
		for i := range d {
			d[i] = make([]int, len(rb)+1)
			d[i][0] = i
		}
		for j := range d[0] {
			d[0][j] = j
		}
		for i := 1; i <= len(ra); i++ {
			for j := 1; j <= len(rb); j++ {
				cost := 1
				if ra[i-1] == rb[j-1] {
					cost = 0
				}
				d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			}
		}
		return d[len(ra)][len(rb)]
	}
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []rune("abc-\u00e9")
	word := func() string {
		r := make([]rune, rng.IntN(9))
		for i := range r {
			r[i] = alphabet[rng.IntN(len(alphabet))]
		}
		return string(r)
	}
	var buf []int
	for range 20000 {
		a, b, limit := word(), word(), rng.IntN(5)
		want := min(naive(a, b), limit+1)
		if got := boundedDistance(a, b, limit, &buf); got != want {
			t.Fatalf("boundedDistance(%q, %q, %d) = %d, want %d", a, b, limit, got, want)
		}
		if got := editDistance(a, b); got != naive(a, b) {
			t.Fatalf("editDistance(%q, %q) = %d", a, b, got)
		}
	}
}

func TestCreateReportsAPathItCannotCheck(t *testing.T) {
	w, root := emptyWiki(t)
	write(t, filepath.Join(root, DirName, PagesDir, "notes"), "a file, not a directory\n")

	// Each failed to stat with an error other than "not exist", which once
	// looped forever looking for a free name.
	for _, n := range []NewPage{{Title: strings.Repeat("x", 300)}, {Title: "Inside", Dir: "notes"}} {
		if _, _, err := w.Create(n); err == nil {
			t.Errorf("Create(%.20q in %q) succeeded", n.Title, n.Dir)
		}
	}
}

func TestABatchThatCannotStageEveryPageWritesNone(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	w, root := moveFixture(t)
	locked := filepath.Join(root, DirName, PagesDir, "notes", "deep")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	// notes/deep/other links to the page, so the move rewrites it, and its
	// directory refuses the temporary file.
	if _, _, err := w.Move("lexer/design-sketch", "archive/sketch"); err == nil {
		t.Fatal("Move succeeded")
	}
	if _, err := os.Stat(pageFile(root, "archive/sketch")); !os.IsNotExist(err) {
		t.Fatalf("the moved page was written: %v", err)
	}
	if got := source(t, root, "index"); !strings.Contains(got, "[[lexer/design-sketch]]") {
		t.Fatalf("index was rewritten:\n%s", got)
	}
	if _, err := w.Page("lexer/design-sketch"); err != nil {
		t.Fatalf("the page left the cache: %v", err)
	}
}

func TestAnUnreadablePageDoesNotFailWritesOrOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n", "locked": "# Locked\n"})
	locked := pageFile(root, "locked")
	write(t, locked, "# Locked, edited\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	_, hash, _ := w.Read("a")
	warn, err := w.commit([]fileWrite{{Page: "a", Base: hash, Data: []byte("# A, mine\n")}})
	if err != nil || len(warn.Skipped) != 1 || warn.Skipped[0] != (Skip{Path: "locked.md", Reason: "permission denied"}) {
		t.Fatalf("commit = %+v, %v", warn, err)
	}
	if p, err := w.Page("a"); err != nil || p.Title != "A, mine" {
		t.Fatalf("Page(a) = %+v, %v: the cache missed the write", p, err)
	}
	other, err := Open(w.Project)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	other.Close()

	// Readable again: the next refresh indexes it.
	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	if ch, err := w.Refresh(); err != nil || strings.Join(ch.Added, ",") != "locked" {
		t.Fatalf("Refresh = %+v, %v", ch, err)
	}
	if s, _ := w.Skipped(); len(s) != 0 {
		t.Fatalf("Skipped = %+v", s)
	}
}

func TestSymlinksAreRefusedAndReported(t *testing.T) {
	w, root := emptyWiki(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "# Secret\n")
	pages := filepath.Join(root, DirName, PagesDir)
	put(t, w, root, map[string]string{"a": "# A\n", "notes/b": "# B\n"})
	for link, target := range map[string]string{
		"secret.md": filepath.Join(outside, "secret.md"), // out of the wiki
		"ext":       outside,
		"alias.md":  "a.md", // inside it
		"inner":     "notes",
		"other.txt": "a.md", // neither a page nor a directory: not reported
	} {
		if err := os.Symlink(target, filepath.Join(pages, link)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Refresh(); err != nil {
		t.Fatal(err)
	}

	skipped, err := w.Skipped()
	var paths []string
	for _, s := range skipped {
		paths = append(paths, s.Path)
		if s.Reason != ErrSymlink.Error() {
			t.Errorf("%s: reason %q", s.Path, s.Reason)
		}
	}
	if err != nil || strings.Join(paths, " ") != "alias.md ext inner secret.md" {
		t.Fatalf("Skipped = %v, %v", skipped, err)
	}
	for _, page := range []string{"secret", "alias", "inner/b"} {
		if _, _, err := w.Read(page); !errors.Is(err, ErrSymlink) {
			t.Errorf("Read(%s) = %v", page, err)
		}
		if _, err := w.Page(page); err == nil {
			t.Errorf("%s was indexed", page)
		}
	}
	if _, _, err := w.Create(NewPage{Title: "Planted", Dir: "ext"}); !errors.Is(err, ErrSymlink) {
		t.Errorf("Create through a symlinked directory = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.md")); !os.IsNotExist(err) {
		t.Errorf("a file appeared outside the wiki: %v", err)
	}
	_, hash, _ := w.Read("a")
	warn, err := w.Write("a", []byte("# A, mine\n"), hash)
	if err != nil || len(warn.Skipped) != 4 || !strings.Contains(warn.String(), "4 files are not in the wiki") {
		t.Fatalf("Write = %+v (%s), %v", warn, warn, err)
	}
	if fi, err := os.Lstat(filepath.Join(pages, "alias.md")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a write replaced a symlink")
	}
}

func TestAnUnreadableDirectoryIsReportedAndRetried(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any directory")
	}
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n", "locked/b": "# B\n"})
	locked := filepath.Join(root, DirName, PagesDir, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	ch, err := w.Refresh()
	if err != nil || strings.Join(ch.Removed, ",") != "locked/b" {
		t.Fatalf("Refresh = %+v, %v", ch, err)
	}
	if s, _ := w.Skipped(); len(s) != 1 || s[0].Path != "locked" || s[0].Reason != "permission denied" {
		t.Fatalf("Skipped = %+v", s)
	}
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if ch, err := w.Refresh(); err != nil || strings.Join(ch.Added, ",") != "locked/b" {
		t.Fatalf("Refresh after chmod = %+v, %v", ch, err)
	}
	if s, _ := w.Skipped(); len(s) != 0 {
		t.Fatalf("Skipped = %+v", s)
	}
}

func TestAMoveThatStopsPartWayNamesWhatLanded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"locked/a": "# A\n"})
	locked := filepath.Join(root, DirName, PagesDir, "locked")
	// The page can be read and copied out, but not removed.
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	_, _, err := w.Move("locked/a", "open/a")
	var partial *ErrPartial
	if !errors.As(err, &partial) || strings.Join(partial.Done, ",") != "open/a" || strings.Join(partial.NotDone, ",") != "locked/a" {
		t.Fatalf("Move = %v", err)
	}
	for _, page := range []string{"locked/a", "open/a"} {
		if _, err := w.Page(page); err != nil {
			t.Errorf("%s is not in the cache: %v", page, err)
		}
	}
}

func TestAWriteThatLandsWithAStaleCacheSucceedsWithAWarning(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"a": "# A\n"})
	// Any refresh that finds a change now fails.
	if _, err := w.db.Exec(`DROP TABLE skipped`); err != nil {
		t.Fatal(err)
	}
	info, warn, err := w.Create(NewPage{Title: "New page"})
	if err != nil || info.Path != "new-page" || info.Title != "New page" || warn.Stale == nil {
		t.Fatalf("Create = %+v, %+v, %v", info, warn, err)
	}
	if !strings.Contains(warn.String(), "cache --rebuild") {
		t.Fatalf("warning: %s", warn)
	}
	if got := source(t, root, "new-page"); got != "# New page\n" {
		t.Fatalf("new-page = %q", got)
	}
}

// A cache another process holds locked is reported, not deleted and rebuilt.
func TestOpenKeepsACacheLockedByAnotherProcess(t *testing.T) {
	w, _ := emptyWiki(t)
	before, err := os.Stat(w.Cache())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := w.db.Begin() // _txlock=immediate: takes the write lock
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	saved := busyTimeout
	busyTimeout = 50
	t.Cleanup(func() { busyTimeout = saved })
	if other, err := Open(w.Project); err == nil {
		other.Close()
		t.Fatal("Open took a cache another connection holds locked")
	} else if !busy(err) {
		t.Fatalf("Open = %v, want a busy error", err)
	}
	after, err := os.Stat(w.Cache())
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("the cache file was replaced: %v", err)
	}
}

// An edit that keeps the size and restores the mtime, as rsync -t and tar do,
// is re-indexed.
func TestRefreshSeesAnEditWithItsMtimeRestored(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{"plan": "# Plan\n\n- [ ] alpha\n"})
	file := pageFile(root, "plan")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond) // past the kernel's timestamp granularity
	write(t, file, "# Plan\n\n- [x] alpha\n")
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	ch, err := w.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Modified) != 1 {
		t.Fatalf("changes = %+v, want plan modified", ch)
	}
	if tasks, _ := w.Tasks(TaskFilter{Page: "plan"}); len(tasks) != 1 || tasks[0].Status != "done" {
		t.Fatalf("tasks = %+v", tasks)
	}
}

// An editor outside gwiki that saves a page after the batch is checked is not
// overwritten: each page is checked again before its rename.
func TestABatchRechecksEachPageBeforeItsRename(t *testing.T) {
	w, root := emptyWiki(t)
	put(t, w, root, map[string]string{
		"target": "# Target\n",
		"source": "See [[target]].\n",
	})
	outside := "See [[target]]. Saved in an editor.\n"
	afterStage = func() { write(t, pageFile(root, "source"), outside) }
	t.Cleanup(func() { afterStage = func() {} })

	_, _, err := w.Move("target", "moved")
	var partial *ErrPartial
	if !errors.As(err, &partial) || strings.Join(partial.Done, ",") != "moved" || strings.Join(partial.NotDone, ",") != "source,target" {
		t.Fatalf("Move = %v", err)
	}
	if got := source(t, root, "source"); got != outside {
		t.Fatalf("the outside save was overwritten:\n%s", got)
	}
	var conflict *ErrConflict
	if !errors.As(partial.Err, &conflict) || conflict.Page != "source" {
		t.Fatalf("partial.Err = %v, want a conflict on source", partial.Err)
	}
}

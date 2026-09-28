package wiki

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/shakfu/gwiki/internal/markdown"
	"gopkg.in/yaml.v3"
)

// NewPage describes a page to create.
type NewPage struct {
	Title string
	Dir   string // under the pages directory; empty for the top
	Task  bool
	Tags  []string
	Body  string

	// Status, Priority, Due and Assignees are written to the front matter of
	// a task page. They are what a migration carries over.
	Status    string
	Priority  string
	Due       string
	Assignees []string
}

// Create writes a new page and returns it. Its file name is the title's slug;
// a name already taken gets -2, -3 and so on.
func (w *Wiki) Create(n NewPage) (PageInfo, Warnings, error) {
	id, src, err := w.newSource(n, nil)
	if err != nil {
		return PageInfo{}, Warnings{}, err
	}
	warn, err := w.commit([]fileWrite{{Page: id, Data: src}})
	if err != nil {
		return PageInfo{}, warn, err
	}
	return w.created(id, strings.TrimSpace(n.Title), warn)
}

// created describes a page a write created. When the cache could not be
// refreshed, the page is not in it; its path and title are what is known.
func (w *Wiki) created(id, title string, warn Warnings) (PageInfo, Warnings, error) {
	if warn.Stale != nil {
		return PageInfo{Path: id, Title: title, Tags: []string{}}, warn, nil
	}
	info, err := w.info(id)
	return info, warn, err
}

// CreateAll writes several pages in one commit, which a migration needs: one
// commit each would re-index the wiki once per page. Pages are written in the
// order given, and a title whose file name is taken gets a numeric suffix.
func (w *Wiki) CreateAll(pages []NewPage) ([]string, Warnings, error) {
	taken := map[string]bool{}
	var writes []fileWrite
	var ids []string
	for _, n := range pages {
		id, src, err := w.newSourceAvoiding(n, taken)
		if err != nil {
			return nil, Warnings{}, err
		}
		taken[id] = true
		ids = append(ids, id)
		writes = append(writes, fileWrite{Page: id, Data: src})
	}
	warn, err := w.commit(writes)
	if err != nil {
		return nil, warn, err
	}
	return ids, warn, nil
}

// newSource chooses a new page's path and writes its source. front, when set,
// adds to the front matter.
func (w *Wiki) newSource(n NewPage, front func(*yaml.Node)) (string, []byte, error) {
	return w.newSourceAvoiding(n, nil, front)
}

// newSourceAvoiding is newSource, also avoiding names this batch has taken.
func (w *Wiki) newSourceAvoiding(n NewPage, taken map[string]bool, front ...func(*yaml.Node)) (string, []byte, error) {
	title := strings.TrimSpace(n.Title)
	if title == "" || strings.ContainsAny(title, "\r\n") {
		return "", nil, errors.New("a page needs a one-line title")
	}
	slug := Slugify(title)
	if slug == "" {
		slug = "page"
	}
	base := slug
	if dir := strings.Trim(n.Dir, "/"); dir != "" {
		base = dir + "/" + slug
	}
	id, err := CleanPath(base)
	if err != nil {
		return "", nil, err
	}
	for i := 2; ; i++ {
		exists, err := w.pageExists(id)
		if err != nil {
			return "", nil, err
		}
		if !exists && !taken[id] {
			break
		}
		id = base + "-" + strconv.Itoa(i)
	}

	src := []byte("# " + title + "\n")
	m := &yaml.Node{Kind: yaml.MappingNode}
	if n.Task {
		setScalar(m, "type", "task")
		status := n.Status
		if status == "" {
			status = "open"
		}
		setScalar(m, "status", status)
		if n.Priority != "" && n.Priority != "none" {
			setScalar(m, "priority", n.Priority)
		}
		if n.Due != "" {
			setScalar(m, "due", n.Due)
		}
		if len(n.Assignees) > 0 {
			seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			for _, who := range n.Assignees {
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: who})
			}
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "assignees"}, seq)
		}
	}
	if len(n.Tags) > 0 {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, t := range n.Tags {
			seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.ToLower(strings.TrimSpace(t))})
		}
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tags"}, seq)
	}
	for _, add := range front {
		if add != nil {
			add(m)
		}
	}
	if len(m.Content) > 0 {
		if src, err = editFront(src, func(fm *yaml.Node) error {
			fm.Content = m.Content
			return nil
		}); err != nil {
			return "", nil, err
		}
	}
	if body := strings.TrimRight(n.Body, "\n"); body != "" {
		src = append(src, "\n"+body+"\n"...)
	}
	return id, src, nil
}

func (w *Wiki) info(id string) (PageInfo, error) {
	rows, err := w.db.Query(`SELECT `+pageColumns+` FROM pages WHERE path = ?`, id)
	if err != nil {
		return PageInfo{}, err
	}
	found, err := scanPages(rows)
	if err != nil {
		return PageInfo{}, err
	}
	if len(found) == 0 {
		return PageInfo{}, fmt.Errorf("no page %q", id)
	}
	return found[0], nil
}

// NormalText is page text as gwiki writes it: \n line breaks and a final
// newline. The browser view and MCP apply it to a whole-page write.
func NormalText(s string) []byte {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s)
}

// Write replaces a page's whole source, given the hash of the source the
// caller read.
func (w *Wiki) Write(page string, src []byte, base string) (Warnings, error) {
	return w.commit([]fileWrite{{Page: page, Base: base, Data: src}})
}

// Replace swaps the one occurrence of old in a page for new, given the hash of
// the source the caller read, and returns the page's new hash.
func (w *Wiki) Replace(page, old, new, base string) (string, Warnings, error) {
	src, hash, err := w.Read(page)
	if err != nil {
		return "", Warnings{}, err
	}
	if hash != base {
		return "", Warnings{}, &ErrConflict{Page: page, Current: src, CurrentHash: hash}
	}
	if old == "" {
		return "", Warnings{}, errors.New("the text to replace is empty")
	}
	at := bytes.Index(src, []byte(old))
	if at < 0 {
		return "", Warnings{}, fmt.Errorf("%s does not contain the text to replace", page)
	}
	if n := occurrences(src, []byte(old)); n > 1 {
		return "", Warnings{}, fmt.Errorf("the text to replace occurs %d times in %s; include more of the text around it", n, page)
	}
	out := append(append(bytes.Clone(src[:at]), new...), src[at+len(old):]...)
	if bytes.Equal(out, src) {
		return hash, Warnings{}, nil
	}
	warn, err := w.Write(page, out, hash)
	if err != nil {
		return "", warn, err
	}
	return Hash(out), warn, nil
}

// occurrences counts the places sub starts in s, overlapping ones included.
func occurrences(s, sub []byte) int {
	n := 0
	for i := bytes.Index(s, sub); i >= 0; {
		n++
		j := bytes.Index(s[i+1:], sub)
		if j < 0 {
			break
		}
		i += j + 1
	}
	return n
}

// readIndexed reads a page for an edit at byte offsets taken from the cache,
// and refuses when the page is no longer what was indexed.
func (w *Wiki) readIndexed(page string) ([]byte, string, error) {
	src, hash, err := w.Read(page)
	if err != nil {
		return nil, "", err
	}
	var cached string
	switch err := w.db.QueryRow(`SELECT hash FROM files WHERE path = ?`, page+".md").Scan(&cached); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, "", err
	}
	if cached != hash {
		return nil, "", &ErrConflict{Page: page, Current: src, CurrentHash: hash}
	}
	return src, hash, nil
}

// SetBody replaces everything after a page's front matter.
func (w *Wiki) SetBody(page, body string) (Warnings, error) {
	src, hash, err := w.Read(page)
	if err != nil {
		return Warnings{}, err
	}
	p := markdown.Parse(src)
	body = strings.TrimRight(body, "\n") + "\n"
	if p.BodyStart > 0 && !strings.HasPrefix(body, "\n") {
		body = "\n" + body
	}
	out := append(bytes.Clone(src[:p.BodyStart]), body...)
	if bytes.Equal(out, src) {
		return Warnings{}, nil
	}
	return w.Write(page, out, hash)
}

// Remove deletes a page. While other pages link to it, it refuses unless force
// is set, and returns those links either way.
func (w *Wiki) Remove(page string, force bool) ([]Link, Warnings, error) {
	back, err := w.Backlinks(page)
	if err != nil {
		return nil, Warnings{}, err
	}
	if len(back) > 0 && !force {
		return back, Warnings{}, fmt.Errorf("%d links reach %s; remove them first or force", len(back), page)
	}
	_, hash, err := w.Read(page)
	if err != nil {
		return back, Warnings{}, err
	}
	warn, err := w.commit([]fileWrite{{Page: page, Base: hash}})
	return back, warn, err
}

// Tag adds and removes front matter tags. Tags are compared without case and
// written lowercase.
func (w *Wiki) Tag(page string, add, remove []string) (Warnings, error) {
	src, hash, err := w.Read(page)
	if err != nil {
		return Warnings{}, err
	}
	out, err := editFront(src, func(m *yaml.Node) error {
		var have []string
		seq := mapValue(m, "tags")
		switch {
		case seq == nil:
		case seq.Kind == yaml.SequenceNode:
			for _, c := range seq.Content {
				have = append(have, strings.ToLower(strings.TrimSpace(c.Value)))
			}
		case seq.Kind == yaml.ScalarNode && seq.Value != "":
			have = []string{strings.ToLower(strings.TrimSpace(seq.Value))}
		}
		want := slices.Clone(have)
		for _, t := range add {
			if t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#"))); t != "" && !slices.Contains(want, t) {
				want = append(want, t)
			}
		}
		want = slices.DeleteFunc(want, func(t string) bool {
			return slices.ContainsFunc(remove, func(r string) bool {
				return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(r), "#"), t)
			})
		})
		if slices.Equal(want, have) {
			return nil
		}
		if len(want) == 0 {
			deleteKey(m, "tags")
			return nil
		}
		style := yaml.FlowStyle
		if seq != nil && seq.Kind == yaml.SequenceNode {
			style = seq.Style
		}
		next := &yaml.Node{Kind: yaml.SequenceNode, Style: style}
		for _, t := range want {
			next.Content = append(next.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t})
		}
		if seq != nil {
			*seq = *next
		} else {
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tags"}, next)
		}
		return nil
	})
	if err != nil || bytes.Equal(out, src) {
		return Warnings{}, err
	}
	return w.Write(page, out, hash)
}

// FindTask resolves a task reference: "page:line" for a checklist item, a task
// page's path, title or file name, or a fragment of a checklist item's text.
func (w *Wiki) FindTask(ref string) (Task, error) {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndexByte(ref, ':'); i > 0 {
		if line, err := strconv.Atoi(ref[i+1:]); err == nil {
			info, err := w.Find(ref[:i])
			if err != nil {
				return Task{}, err
			}
			tasks, err := w.Tasks(TaskFilter{Page: info.Path})
			if err != nil {
				return Task{}, err
			}
			for _, t := range tasks {
				if t.Line == line {
					return t, nil
				}
			}
			return Task{}, fmt.Errorf("no checklist item on line %d of %s", line, info.Path)
		}
	}

	if info, err := w.Find(ref); err == nil && info.Type == "task" {
		tasks, err := w.Tasks(TaskFilter{Page: info.Path})
		if err != nil {
			return Task{}, err
		}
		for _, t := range tasks {
			if t.Line == 0 {
				return t, nil
			}
		}
	}

	tasks, err := w.Tasks(TaskFilter{})
	if err != nil {
		return Task{}, err
	}
	var found []Task
	for _, t := range tasks {
		if t.Line > 0 && strings.Contains(strings.ToLower(t.Text), strings.ToLower(ref)) {
			found = append(found, t)
		}
	}
	switch len(found) {
	case 0:
		return Task{}, fmt.Errorf("no task matches %q", ref)
	case 1:
		return found[0], nil
	}
	var names []string
	for _, t := range found {
		names = append(names, fmt.Sprintf("%s:%d (%s)", t.Page, t.Line, t.Text))
	}
	return Task{}, fmt.Errorf("%q could mean %s", ref, strings.Join(names, ", "))
}

// SetTaskStatus sets a task page's status, or ticks or clears a checklist
// item, which has no doing.
func (w *Wiki) SetTaskStatus(t Task, status string) (Warnings, error) {
	switch status {
	case "open", "doing", "done":
	default:
		return Warnings{}, fmt.Errorf("status is open, doing or done, not %q", status)
	}
	read := w.Read
	if t.Line > 0 {
		read = w.readIndexed
	}
	src, hash, err := read(t.Page)
	if err != nil {
		return Warnings{}, err
	}

	if t.Line == 0 {
		out, err := editFront(src, func(m *yaml.Node) error {
			setScalar(m, "status", status)
			return nil
		})
		if err != nil || bytes.Equal(out, src) {
			return Warnings{}, err
		}
		return w.Write(t.Page, out, hash)
	}

	if status == "doing" {
		return Warnings{}, errors.New("a checklist item is open or done; promote it to a task page for doing")
	}
	box := byte(' ')
	if status == "done" {
		box = 'x'
	}
	if err := w.checkItem(t, src, hash); err != nil {
		return Warnings{}, err
	}
	if (src[t.Box] == ' ') == (box == ' ') {
		return Warnings{}, nil
	}
	out := bytes.Clone(src)
	out[t.Box] = box
	return w.Write(t.Page, out, hash)
}

// checkItem refuses a checklist item whose box no longer holds its text in
// src, which readIndexed has matched to the cache. A Task held across a
// refresh can point at another item or past the end of the page.
func (w *Wiki) checkItem(t Task, src []byte, hash string) error {
	var text string
	err := w.db.QueryRow(`SELECT text FROM checklist WHERE page = ? AND box = ?`, t.Page, t.Box).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) || err == nil && text != t.Text {
		return &ErrConflict{Page: t.Page, Current: src, CurrentHash: hash}
	}
	return err
}

var dueWord = regexp.MustCompile(`\s*\bdue:(\d{4}-\d{2}-\d{2})\b`)

// Promote turns a checklist item into a task page in dir, and replaces the
// item with a link to it.
func (w *Wiki) Promote(t Task, dir string) (PageInfo, Warnings, error) {
	if t.Line == 0 {
		return PageInfo{}, Warnings{}, fmt.Errorf("%s is already a task page", t.Page)
	}
	src, hash, err := w.readIndexed(t.Page)
	if err != nil {
		return PageInfo{}, Warnings{}, err
	}
	if err := w.checkItem(t, src, hash); err != nil {
		return PageInfo{}, Warnings{}, err
	}
	lineEnd := bytes.IndexByte(src[t.Box:], '\n')
	if lineEnd < 0 {
		lineEnd = len(src)
	} else {
		lineEnd += t.Box
	}
	text := strings.TrimSpace(string(src[t.Box+2 : lineEnd]))
	title := strings.TrimSpace(dueWord.ReplaceAllString(text, ""))
	if title == "" {
		return PageInfo{}, Warnings{}, errors.New("the item has no text to title a page")
	}

	id, page, err := w.newSource(NewPage{Title: title, Dir: dir, Task: true}, func(m *yaml.Node) {
		if t.Status == "done" {
			setScalar(m, "status", "done")
		}
		if d := dueWord.FindStringSubmatch(text); d != nil {
			setScalar(m, "due", d[1])
		}
	})
	if err != nil {
		return PageInfo{}, Warnings{}, err
	}

	// A title that will name only the new page is the shorter link.
	link := "[[" + id + "|" + title + "]]"
	if ix, err := w.readIndex(); err == nil {
		ix.addEntry(id, title)
		if got, status := ix.wiki(title); status == StatusOK && got == id {
			link = "[[" + title + "]]"
		}
	}
	out, err := applySpans(src, []span{{start: t.Box - 1, end: lineEnd, old: string(src[t.Box-1 : lineEnd]), new: link}})
	if err != nil {
		return PageInfo{}, Warnings{}, err
	}
	warn, err := w.commit([]fileWrite{{Page: id, Data: page}, {Page: t.Page, Base: hash, Data: out}})
	if err != nil {
		return PageInfo{}, warn, err
	}
	return w.created(id, title, warn)
}

// readIndex loads the page index outside a refresh.
func (w *Wiki) readIndex() (*index, error) {
	tx, err := w.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ix, err := loadIndex(tx)
	if err != nil {
		return nil, err
	}
	// Headings are loaded on demand through the transaction, which ends here;
	// load them all now.
	rows, err := tx.Query(`SELECT page, slug FROM headings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p, slug string
		if err := rows.Scan(&p, &slug); err != nil {
			return nil, err
		}
		if ix.slugs[p] == nil {
			ix.slugs[p] = map[string]bool{}
		}
		ix.slugs[p][slug] = true
	}
	for p := range ix.exact {
		if ix.slugs[p] == nil {
			ix.slugs[p] = map[string]bool{}
		}
	}
	ix.tx = nil
	return ix, rows.Err()
}

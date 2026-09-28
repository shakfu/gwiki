package webwiki

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakfu/gwiki/internal/markdown"
	"github.com/shakfu/gwiki/internal/wiki"
)

// writeJSON sends a value, or an error when it cannot be encoded.
func writeJSON(w http.ResponseWriter, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "could not encode the response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

func fail(w http.ResponseWriter, err error, status int) {
	writeStatus(w, status, map[string]string{"error": err.Error()})
}

func writeStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ---------------------------------------------------------------- reading

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type response struct {
		Name     string            `json:"name"`
		Pages    int               `json:"pages"`
		Recent   []wiki.Change     `json:"recent"`
		Broken   int               `json:"broken"`
		Skipped  []wiki.Skip       `json:"skipped"`
		Orphans  []wiki.PageInfo   `json:"orphans"`
		DeadEnds []wiki.PageInfo   `json:"deadEnds"`
		Tasks    []wiki.Task       `json:"tasks"`
		Overdue  int               `json:"overdue"`
		DueSoon  int               `json:"dueSoon"`
		Dirs     []wiki.Count      `json:"dirs"`
		Tags     []wiki.Count      `json:"tags"`
		Hubs     []wiki.Count      `json:"hubs"`
		Titles   map[string]string `json:"titles"`
	}
	out := response{Name: s.w.Config.Name, Titles: map[string]string{}}

	pages, err := s.w.Pages("", "")
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	out.Pages = len(pages)
	dirs := map[string]int{}
	for _, p := range pages {
		out.Titles[p.Path] = p.Title
		top := ""
		if d := path.Dir(p.Path); d != "." {
			top = strings.SplitN(d, "/", 2)[0]
		}
		dirs[top]++
	}
	for name, n := range dirs {
		out.Dirs = append(out.Dirs, wiki.Count{Name: name, Pages: n})
	}
	sort.Slice(out.Dirs, func(i, j int) bool {
		if out.Dirs[i].Pages != out.Dirs[j].Pages {
			return out.Dirs[i].Pages > out.Dirs[j].Pages
		}
		return out.Dirs[i].Name < out.Dirs[j].Name
	})

	for _, load := range []func() error{
		func() (err error) { out.Recent, err = s.w.Recent(12); return },
		func() (err error) { out.Broken, err = s.w.BrokenCount(); return },
		func() (err error) { out.Skipped, err = s.w.Skipped(); return },
		func() (err error) { out.Orphans, err = s.w.Orphans(); return },
		func() (err error) { out.DeadEnds, err = s.w.DeadEnds(); return },
		func() (err error) { out.Tags, err = s.w.TagCounts(); return },
		func() (err error) { out.Hubs, err = s.w.Hubs(8); return },
		func() (err error) { out.Tasks, err = s.w.Tasks(wiki.TaskFilter{Status: "open"}); return },
	} {
		if err := load(); err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
	}

	today := time.Now().Format("2006-01-02")
	soon := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	for _, t := range out.Tasks {
		switch {
		case t.Due == "":
		case t.Due < today:
			out.Overdue++
		case t.Due <= soon:
			out.DueSoon++
		}
	}
	sort.SliceStable(out.Tasks, func(i, j int) bool {
		a, b := out.Tasks[i], out.Tasks[j]
		if (a.Due == "") != (b.Due == "") {
			return a.Due != ""
		}
		return a.Due < b.Due
	})
	writeJSON(w, out)
}

func (s *Server) handlePages(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pages, err := s.w.Pages(r.URL.Query().Get("dir"), r.URL.Query().Get("tag"))
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	// The name comes with the list so that a page opened directly, without
	// the overview, still has it for the header; stale, so that every view
	// shows a failed refresh.
	writeJSON(w, map[string]any{"name": s.w.Config.Name, "pages": pages, "stale": s.stale})
}

// page is one page with everything the browser draws.
type page struct {
	wiki.PageInfo
	Hash      string      `json:"hash"`
	HTML      string      `json:"html"`
	Source    string      `json:"source"`
	Links     []link      `json:"links"`
	Backlinks []link      `json:"backlinks"`
	Tasks     []wiki.Task `json:"tasks"`
	Broken    int         `json:"broken"`
}

// link is a link as the browser lists it.
type link struct {
	Page     string `json:"page"`
	Line     int    `json:"line"`
	Written  string `json:"written"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Resolved string `json:"resolved"`
	Href     string `json:"href"`
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := wiki.CleanPath(r.URL.Query().Get("p"))
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	src, hash, err := s.w.Read(id)
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}
	full, err := s.w.Page(id)
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}

	out := page{PageInfo: full.PageInfo, Hash: hash, Source: string(src), Tasks: full.Tasks,
		Links: []link{}, Backlinks: []link{}}

	// Links resolved for this rendering, keyed as the renderer asks for them.
	byKey := map[string]wiki.Link{}
	for _, l := range full.Links {
		byKey[l.Form+"\x00"+l.Target+"\x00"+l.Anchor] = l
		out.Links = append(out.Links, s.link(l))
		if l.Status != wiki.StatusOK {
			out.Broken++
		}
	}
	headings := map[string][]wiki.Heading{}
	for _, l := range full.Backlinks {
		bl := s.link(l)
		if bl.Href, err = s.backHref(l, headings); err != nil {
			fail(w, err, http.StatusInternalServerError)
			return
		}
		out.Backlinks = append(out.Backlinks, bl)
	}

	html, err := markdown.HTML(src, func(l markdown.Link) markdown.Target {
		c, ok := byKey[string(l.Form)+"\x00"+l.Target+"\x00"+l.Anchor]
		if !ok {
			return markdown.Target{}
		}
		t := markdown.Target{Href: s.href(c), Broken: c.Status != wiki.StatusOK}
		switch {
		case c.Status != wiki.StatusOK:
			t.Title = c.Status
		case c.Kind == wiki.KindPage || c.Kind == wiki.KindHeading:
			t.Title = c.Resolved
		case t.Href == blocked:
			t.Title = "only http, https and mailto links open here"
		}
		return t
	})
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	out.HTML = string(html)
	writeJSON(w, out)
}

// blocked is the href of an external link whose scheme the page will not open.
const blocked = "#/"

// safeExternal allows http, https, mailto and a protocol-relative URL. Other
// schemes, such as javascript: and data:, can run script in the page.
func safeExternal(u string) bool {
	if strings.HasPrefix(u, "//") {
		return true
	}
	scheme, _, ok := strings.Cut(u, ":")
	if !ok {
		return false
	}
	switch strings.ToLower(scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// href is where the browser sends a link.
func (s *Server) href(l wiki.Link) string {
	switch {
	case l.Kind == wiki.KindExternal && l.Resolved != "" && !safeExternal(l.Resolved):
		return blocked
	case l.Kind == wiki.KindExternal:
		return l.Resolved
	case l.Status != wiki.StatusOK && l.Status != wiki.StatusMissingHeading:
		return "#/broken"
	case l.Kind == wiki.KindPage, l.Kind == wiki.KindHeading:
		href := "#/page/" + url.PathEscape(l.Resolved)
		if l.Anchor != "" {
			href += "#" + markdown.Slug(l.Anchor)
		}
		return href
	case l.Kind == wiki.KindFile, l.Kind == wiki.KindLine:
		href := "#/file/" + url.PathEscape(l.Resolved)
		if l.Anchor != "" {
			href += "#" + l.Anchor
		}
		return href
	}
	return ""
}

// backHref leads to the page a backlink is on, at the heading above it, since
// the rendered page has no line numbers. headings caches each page's headings.
func (s *Server) backHref(l wiki.Link, headings map[string][]wiki.Heading) (string, error) {
	hs, ok := headings[l.Page]
	if !ok {
		var err error
		if hs, err = s.w.Headings(l.Page); err != nil {
			return "", err
		}
		headings[l.Page] = hs
	}
	href := "#/page/" + url.PathEscape(l.Page)
	slug := ""
	for _, h := range hs {
		if h.Line > l.Line {
			break
		}
		slug = h.Slug
	}
	if slug != "" {
		href += "#" + slug
	}
	return href, nil
}

func (s *Server) link(l wiki.Link) link {
	return link{Page: l.Page, Line: l.Line, Written: l.Written(), Kind: l.Kind,
		Status: l.Status, Resolved: l.Resolved, Href: s.href(l)}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || limit <= 0 {
		limit = 30
	}
	hits, err := s.w.Search(r.URL.Query().Get("q"), limit)
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	type result struct {
		wiki.PageInfo
		Snippet string `json:"snippet"`
	}
	out := []result{}
	for _, h := range hits {
		// The markers become tags the page styles; everything else is escaped
		// by the browser when it sets the text.
		out = append(out, result{PageInfo: h.PageInfo, Snippet: h.Snippet})
	}
	writeJSON(w, out)
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	broken, err := s.w.Check()
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	out := []link{}
	for _, l := range broken {
		out = append(out, s.link(l))
	}
	writeJSON(w, out)
}

// handleFile serves a source file the wiki links to, so a link into the code
// opens beside the page. Only files inside the repository are read.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	repo := s.w.Repo
	s.mu.Unlock()

	rel := path.Clean("/" + r.URL.Query().Get("p"))[1:]
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	inside, err := filepath.Rel(repo, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		fail(w, errors.New("that path is outside the repository"), http.StatusBadRequest)
		return
	}
	// Read through the repository root, so a symlink cannot lead outside it.
	root, err := os.OpenRoot(repo)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	defer root.Close()
	info, err := root.Stat(inside)
	if err != nil {
		fail(w, err, http.StatusNotFound)
		return
	}
	const maxFile = 1 << 20
	if info.IsDir() || info.Size() > maxFile {
		fail(w, errors.New("only files under 1 MB are shown"), http.StatusBadRequest)
		return
	}
	raw, err := root.ReadFile(inside)
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"path": rel, "text": string(raw)})
}

// ---------------------------------------------------------------- writing

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Page string `json:"page"`
		Base string `json:"base"`
		Text string `json:"text"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := wiki.CleanPath(in.Page)
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	text := strings.ReplaceAll(in.Text, "\r\n", "\n")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	var conflict *wiki.ErrConflict
	warn, err := s.w.Write(id, []byte(text), in.Base)
	switch {
	case errors.As(err, &conflict):
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": err.Error(), "current": string(conflict.Current), "hash": conflict.CurrentHash,
		})
		return
	case err != nil:
		fail(w, err, http.StatusBadRequest)
		return
	}
	s.bump()
	writeJSON(w, map[string]string{"hash": wiki.Hash([]byte(text)), "warning": warning(warn)})
}

// warning is what a write that landed left wrong, or empty.
func warning(warn wiki.Warnings) string {
	if warn.Empty() {
		return ""
	}
	return warn.String()
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Dir   string `json:"dir"`
		Task  bool   `json:"task"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	info, warn, err := s.w.Create(wiki.NewPage{Title: in.Title, Dir: in.Dir, Task: in.Task})
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	s.bump()
	writeJSON(w, struct {
		wiki.PageInfo
		Warning string `json:"warning"`
	}{info, warning(warn)})
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Page   string `json:"page"`
		Line   int    `json:"line"`
		Text   string `json:"text"`
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := wiki.CleanPath(in.Page)
	if err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	tasks, err := s.w.Tasks(wiki.TaskFilter{Page: id})
	if err != nil {
		fail(w, err, http.StatusInternalServerError)
		return
	}
	for _, t := range tasks {
		if t.Line != in.Line {
			continue
		}
		// The text guards against a line that now holds another item.
		if in.Text != "" && strings.TrimSpace(in.Text) != t.Text {
			fail(w, errors.New("that line now holds "+strconv.Quote(t.Text)+"; reload the page"), http.StatusConflict)
			return
		}
		warn, err := s.w.SetTaskStatus(t, in.Status)
		if err != nil {
			fail(w, err, http.StatusConflict)
			return
		}
		s.bump()
		writeJSON(w, map[string]string{"status": in.Status, "warning": warning(warn)})
		return
	}
	fail(w, errors.New("no task on that line"), http.StatusNotFound)
}

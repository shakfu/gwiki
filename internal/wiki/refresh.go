package wiki

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/shakfu/gwiki/internal/markdown"
)

// Changes reports what a refresh found.
type Changes struct {
	Added, Modified, Removed []string
	Renamed                  [][2]string // old path, new path
}

// Empty reports whether nothing changed.
func (c Changes) Empty() bool {
	return len(c.Added)+len(c.Modified)+len(c.Removed) == 0
}

type fileStat struct {
	rel                string // relative to the pages directory, slash-separated, with .md
	size, mtime, ctime int64
}

// changed reports whether f differs from the file as last indexed. ctime
// catches an edit whose mtime was restored, as rsync -t and tar do.
func (f fileStat) changed(k knownFile) bool {
	return k.size != f.size || k.mtime != f.mtime || k.ctime != f.ctime
}

// Skip is a file in the pages directory that the wiki leaves out.
type Skip struct {
	Path   string `json:"path"` // relative to the pages directory, slash-separated
	Reason string `json:"reason"`

	// retry marks an error, such as a page that could not be read, which the
	// next refresh tries again. A symlink stays skipped until it changes.
	retry bool
}

func skipErr(rel string, err error) Skip {
	if pe, ok := err.(*os.PathError); ok {
		err = pe.Err
	}
	return Skip{Path: rel, Reason: err.Error(), retry: true}
}

func sameSkips(a, b []Skip) bool {
	return slices.EqualFunc(a, b, func(x, y Skip) bool { return x.Path == y.Path && x.Reason == y.Reason })
}

// Skipped lists the files the last refresh left out.
func (w *Wiki) Skipped() ([]Skip, error) { return skippedIn(w.db) }

type parsed struct {
	fileStat
	hash string
	src  []byte
	page *markdown.Page
	err  error
}

// Refresh brings the cache up to date with the pages on disk.
//
// Pages are compared by size, modification time and inode change time; only
// those that differ are read. A fingerprint of every page's name, size and
// times answers the common
// case, nothing changed, without reading the file table. A write transaction is
// taken only when something changed.
//
// A file that cannot be read, and a symlink, are left out as if absent and
// listed in Skipped. While a read failed, the stored fingerprint is marked so
// that it never matches, and every refresh tries again.
func (w *Wiki) Refresh() (Changes, error) {
	var ch Changes
	root, err := os.OpenRoot(w.PagesPath())
	if os.IsNotExist(err) {
		return ch, ErrNotFound
	} else if err != nil {
		return ch, err
	}
	defer root.Close()
	found, skipped, err := w.scan(root)
	if err != nil {
		return ch, err
	}
	print := fingerprint(found, skipped)
	if stored, err := storedFingerprint(w.db); err != nil || stored == print {
		return ch, err
	}

	// Under the write lock from here: another process may have indexed the
	// same change while this one waited, and the file table read must be the
	// one this refresh writes against.
	tx, err := w.db.Begin()
	if err != nil {
		return ch, fmt.Errorf("write %s: %w", w.Cache(), err)
	}
	defer tx.Rollback()
	stored, err := storedFingerprint(tx)
	if err != nil || stored == print {
		return ch, err
	}

	known, err := knownFiles(tx)
	if err != nil {
		return ch, err
	}
	var todo []fileStat
	for _, f := range found {
		if k, ok := known[f.rel]; !ok || f.changed(k) {
			todo = append(todo, f)
		}
	}
	unread := map[string]bool{}
	pages := slices.DeleteFunc(parseAll(root, todo), func(p parsed) bool {
		if p.err != nil {
			unread[p.rel] = true
			skipped = append(skipped, skipErr(p.rel, p.err))
		}
		return p.err != nil
	})
	slices.SortFunc(skipped, func(a, b Skip) int { return strings.Compare(a.Path, b.Path) })
	found = slices.DeleteFunc(found, func(f fileStat) bool { return unread[f.rel] })
	if slices.ContainsFunc(skipped, func(s Skip) bool { return s.retry }) {
		print = "retry " + print
	}

	for _, f := range found {
		k, ok := known[f.rel]
		switch {
		case !ok:
			ch.Added = append(ch.Added, pageID(f.rel))
		case f.changed(k):
			ch.Modified = append(ch.Modified, pageID(f.rel))
		}
		delete(known, f.rel)
	}
	for rel := range known {
		ch.Removed = append(ch.Removed, pageID(rel))
	}
	sort.Strings(ch.Added)
	sort.Strings(ch.Modified)
	sort.Strings(ch.Removed)
	was, err := skippedIn(tx)
	if err != nil {
		return ch, err
	}
	if ch.Empty() && stored == print && sameSkips(was, skipped) {
		return ch, nil // the same files still fail to read
	}

	wr, err := w.newWriter(tx)
	if err != nil {
		return ch, err
	}
	defer wr.close()

	for _, id := range ch.Removed {
		if err := wr.forget(id); err != nil {
			return ch, err
		}
		if _, err := tx.Exec(`INSERT INTO gone (path, hash) VALUES (?, ?)`, id, known[id+".md"].hash); err != nil {
			return ch, err
		}
	}
	// Bounded: only recent removals are worth offering as renames.
	if _, err := tx.Exec(`DELETE FROM gone WHERE rowid <= (SELECT max(rowid) FROM gone) - 1000`); err != nil {
		return ch, err
	}

	added := map[string]bool{}
	for _, id := range ch.Added {
		added[id] = true
	}
	for _, id := range ch.Modified {
		if err := wr.forget(id); err != nil {
			return ch, err
		}
	}
	// The index is every page as it will be after this refresh, so each new
	// link is written with its final status.
	if wr.ix, err = loadIndex(tx); err != nil {
		return ch, err
	}
	for _, p := range pages {
		wr.ix.add(pageID(p.rel), p.page)
	}
	for _, p := range pages {
		id := pageID(p.rel)
		if err := wr.insert(p); err != nil {
			return ch, fmt.Errorf("index %s: %w", p.rel, err)
		}
		if !added[id] {
			continue
		}
		var old string
		switch err := tx.QueryRow(`SELECT path FROM gone WHERE hash = ? AND path != ? ORDER BY rowid DESC LIMIT 1`, p.hash, id).Scan(&old); err {
		case nil:
			ch.Renamed = append(ch.Renamed, [2]string{old, id})
			if _, err := tx.Exec(`INSERT INTO renames (old, new) VALUES (?, ?)`, old, id); err != nil {
				return ch, err
			}
			if _, err := tx.Exec(`DELETE FROM gone WHERE path = ?`, old); err != nil {
				return ch, err
			}
		case sql.ErrNoRows:
		default:
			return ch, err
		}
	}

	if err := wr.resolve(); err != nil {
		return ch, err
	}
	if _, err := tx.Exec(`DELETE FROM skipped`); err != nil {
		return ch, err
	}
	for _, s := range skipped {
		if _, err := tx.Exec(`INSERT INTO skipped (path, reason) VALUES (?, ?)`, s.Path, s.Reason); err != nil {
			return ch, err
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (k, v) VALUES ('fingerprint', ?)`, print); err != nil {
		return ch, err
	}
	if err := tx.Commit(); err != nil {
		return ch, fmt.Errorf("write %s: %w", w.Cache(), err)
	}
	return ch, nil
}

// fingerprint summarises every page's name, size and modification time, and
// the names of the symlinks. The per-file hashes are summed, so the order of
// the scan does not matter.
func fingerprint(files []fileStat, skipped []Skip) string {
	files = slices.Clone(files)
	for _, s := range skipped {
		if !s.retry {
			files = append(files, fileStat{rel: s.Path, size: -1})
		}
	}
	var sum uint64
	var buf [24]byte
	for _, f := range files {
		h := fnv.New64a()
		h.Write([]byte(f.rel))
		for i := 0; i < 8; i++ {
			buf[i] = byte(f.size >> (8 * i))
			buf[8+i] = byte(f.mtime >> (8 * i))
			buf[16+i] = byte(f.ctime >> (8 * i))
		}
		h.Write(buf[:])
		sum += h.Sum64()
	}
	return strconv.Itoa(len(files)) + ":" + strconv.FormatUint(sum, 16)
}

// scan lists every page with its size and modification time, and the files it
// leaves out. Directories are listed in one goroutine and files are stat'ed on
// a pool, which measured three times faster than filepath.WalkDir on 5,000
// pages. Hidden entries are skipped without a report.
func (w *Wiki) scan(root *os.Root) ([]fileStat, []Skip, error) {
	var rels []string
	var skipped []Skip
	dirs := []string{""}
	for len(dirs) > 0 {
		dir := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]
		entries, err := readDir(root, dir)
		if err != nil {
			if dir == "" {
				return nil, nil, err
			}
			skipped = append(skipped, skipErr(dir, err))
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			rel := path.Join(dir, name)
			switch {
			case e.Type()&os.ModeSymlink != 0:
				// Reported when it would have been a page or a directory.
				info, err := os.Stat(filepath.Join(w.PagesPath(), filepath.FromSlash(rel)))
				if strings.HasSuffix(name, ".md") || err == nil && info.IsDir() {
					skipped = append(skipped, Skip{Path: rel, Reason: ErrSymlink.Error()})
				}
			case e.IsDir():
				dirs = append(dirs, rel)
			case strings.HasSuffix(name, ".md"):
				rels = append(rels, rel)
			}
		}
	}

	out := make([]fileStat, len(rels))
	skips := make([]Skip, len(rels))
	parallel(len(rels), func(i int) {
		info, err := root.Lstat(filepath.FromSlash(rels[i]))
		switch {
		case os.IsNotExist(err): // removed since it was listed
		case err != nil:
			skips[i] = skipErr(rels[i], err)
		case !info.Mode().IsRegular():
			skips[i] = Skip{Path: rels[i], Reason: "not a regular file"}
		default:
			out[i] = fileStat{rel: rels[i], size: info.Size(), mtime: info.ModTime().UnixNano(), ctime: ctime(info)}
		}
	})
	kept := out[:0]
	for i := range out {
		if out[i].rel != "" {
			kept = append(kept, out[i])
		}
		if skips[i].Path != "" {
			skipped = append(skipped, skips[i])
		}
	}
	return kept, skipped, nil
}

type knownFile struct {
	size, mtime, ctime int64
	hash               string
}

func skippedIn(q interface {
	Query(string, ...any) (*sql.Rows, error)
}) ([]Skip, error) {
	rows, err := q.Query(`SELECT path, reason FROM skipped ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skip{}
	for rows.Next() {
		var s Skip
		if err := rows.Scan(&s.Path, &s.Reason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func storedFingerprint(q interface {
	QueryRow(string, ...any) *sql.Row
}) (string, error) {
	var v string
	if err := q.QueryRow(`SELECT v FROM meta WHERE k = 'fingerprint'`).Scan(&v); err != nil && err != sql.ErrNoRows {
		return "", err
	}
	return v, nil
}

func knownFiles(tx *sql.Tx) (map[string]knownFile, error) {
	rows, err := tx.Query(`SELECT path, size, mtime, ctime, hash FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]knownFile{}
	for rows.Next() {
		var rel string
		var k knownFile
		if err := rows.Scan(&rel, &k.size, &k.mtime, &k.ctime, &k.hash); err != nil {
			return nil, err
		}
		out[rel] = k
	}
	return out, rows.Err()
}

func readDir(root *os.Root, dir string) ([]os.DirEntry, error) {
	f, err := root.Open(filepath.FromSlash(path.Join(".", dir)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func parseAll(root *os.Root, files []fileStat) []parsed {
	out := make([]parsed, len(files))
	parallel(len(files), func(i int) {
		p := parsed{fileStat: files[i]}
		p.src, p.err = root.ReadFile(filepath.FromSlash(files[i].rel))
		if p.err == nil {
			sum := sha256.Sum256(p.src)
			p.hash = hex.EncodeToString(sum[:])
			p.page = markdown.Parse(p.src)
		}
		out[i] = p
	})
	return out
}

// parallel runs fn for 0..n-1 on up to 16 goroutines, each taking indices from
// a shared counter.
func parallel(n int, fn func(int)) {
	workers := min(runtime.GOMAXPROCS(0), 16, n)
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := int(next.Add(1) - 1); i < n; i = int(next.Add(1) - 1) {
				fn(i)
			}
		}()
	}
	wg.Wait()
}

func pageID(rel string) string { return strings.TrimSuffix(rel, ".md") }

// writer holds one refresh's prepared statements, and the keys of the pages it
// touched, which decide the links to re-resolve.
type writer struct {
	w          *Wiki
	tx         *sql.Tx
	stmts      map[string]*sql.Stmt
	lineCounts map[string]int

	// Lowercase paths, titles and stems, and exact paths, of every page added,
	// changed or removed, before and after the change.
	paths, titles, stems, ids map[string]bool

	// ix resolves page links as they are inserted.
	ix *index
}

var writerStatements = map[string]string{
	"page":       `SELECT num, title, stem FROM pages WHERE path = ?`,
	"del fts":    `DELETE FROM pages_fts WHERE rowid = ?`,
	"del page":   `DELETE FROM pages WHERE path = ?`,
	"del head":   `DELETE FROM headings WHERE page = ?`,
	"del tags":   `DELETE FROM tags WHERE page = ?`,
	"del assign": `DELETE FROM assignees WHERE page = ?`,
	"del links":  `DELETE FROM links WHERE page = ?`,
	"del check":  `DELETE FROM checklist WHERE page = ?`,
	"del file":   `DELETE FROM files WHERE path = ?`,
	"ins file":   `INSERT INTO files (path, size, mtime, ctime, hash) VALUES (?, ?, ?, ?, ?)`,
	"ins page":   `INSERT INTO pages (path, title, stem, type, status, priority, due, body) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	"ins head":   `INSERT INTO headings (page, slug, text, level, line) VALUES (?, ?, ?, ?, ?)`,
	"ins tag":    `INSERT INTO tags (page, tag) VALUES (?, ?)`,
	"ins assign": `INSERT INTO assignees (page, who) VALUES (?, ?)`,
	"ins fts":    `INSERT INTO pages_fts (rowid, title, headings, tags, body) VALUES (?, ?, ?, ?, ?)`,
	"ins link": `INSERT INTO links (page, line, col, start, stop, dest_start, dest_stop, form, image, ref,
			label, target, anchor, kind, resolved, line_from, line_to, status, key_path, key_hyph, key_stem)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	"ins check": `INSERT INTO checklist (page, line, text, done, due, box) VALUES (?, ?, ?, ?, ?, ?)`,
}

func (w *Wiki) newWriter(tx *sql.Tx) (*writer, error) {
	wr := &writer{w: w, tx: tx, stmts: map[string]*sql.Stmt{}, lineCounts: map[string]int{},
		paths: map[string]bool{}, titles: map[string]bool{}, stems: map[string]bool{}, ids: map[string]bool{}}
	for name, q := range writerStatements {
		s, err := tx.Prepare(q)
		if err != nil {
			wr.close()
			return nil, err
		}
		wr.stmts[name] = s
	}
	return wr, nil
}

func (wr *writer) close() {
	for _, s := range wr.stmts {
		s.Close()
	}
}

func (wr *writer) exec(name string, args ...any) error {
	_, err := wr.stmts[name].Exec(args...)
	return err
}

func (wr *writer) touch(id, title, stem string) {
	wr.ids[id] = true
	wr.paths[strings.ToLower(id)] = true
	if dir := ReadmeDir(id); dir != "" {
		wr.paths[strings.ToLower(dir)] = true // [[dir]] names the page
	}
	wr.titles[strings.ToLower(title)] = true
	wr.stems[stem] = true
}

// forget deletes every row of a page, remembering its keys.
func (wr *writer) forget(id string) error {
	var num int64
	var title, stem string
	switch err := wr.stmts["page"].QueryRow(id).Scan(&num, &title, &stem); err {
	case nil:
		wr.touch(id, title, stem)
		if err := wr.exec("del fts", num); err != nil {
			return err
		}
	case sql.ErrNoRows:
	default:
		return err
	}
	for _, name := range []string{"del page", "del head", "del tags", "del assign", "del links", "del check"} {
		if err := wr.exec(name, id); err != nil {
			return err
		}
	}
	return wr.exec("del file", id+".md")
}

func (wr *writer) insert(p parsed) error {
	id := pageID(p.rel)
	pg := p.page
	f := pg.Front
	if f == nil {
		f = &markdown.Front{}
	}
	title := pg.Title
	if title == "" {
		title = pageName(id)
	}
	stem := pageStem(id)
	wr.touch(id, title, stem)
	body := string(p.src[pg.BodyStart:])

	if err := wr.exec("ins file", p.rel, p.size, p.mtime, p.ctime, p.hash); err != nil {
		return err
	}
	res, err := wr.stmts["ins page"].Exec(id, title, stem, strings.ToLower(f.Type), strings.ToLower(f.Status),
		strings.ToLower(f.Priority), f.Due, body)
	if err != nil {
		return err
	}
	num, err := res.LastInsertId()
	if err != nil {
		return err
	}

	var headings []string
	for _, h := range pg.Headings {
		headings = append(headings, h.Text)
		if err := wr.exec("ins head", id, h.Slug, h.Text, h.Level, h.Line); err != nil {
			return err
		}
	}
	var tags []string
	for _, t := range f.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tags = append(tags, t)
			if err := wr.exec("ins tag", id, t); err != nil {
				return err
			}
		}
	}
	for _, a := range f.Assignees {
		if err := wr.exec("ins assign", id, strings.TrimSpace(a)); err != nil {
			return err
		}
	}
	if err := wr.exec("ins fts", num, title, strings.Join(headings, "\n"), strings.Join(tags, " "), body); err != nil {
		return err
	}

	for _, l := range pg.Links {
		c := wr.w.classify(id, l, wr.lineCounts)
		if c.kind == KindPage || c.kind == KindHeading {
			if c.resolved, c.status, err = wr.ix.resolve(id, string(l.Form), l.Target, l.Anchor, c.resolved); err != nil {
				return err
			}
		}
		var kp, kh, ks string
		if l.Form == markdown.FormWiki && l.Target != "" {
			kp, kh, ks = wikiKeys(l.Target)
		}
		if err := wr.exec("ins link", id, l.Line, l.Col, l.Start, l.End, l.DestStart, l.DestEnd, string(l.Form), l.Image, l.Ref,
			l.Label, l.Target, l.Anchor, c.kind, c.resolved, c.from, c.to, c.status, kp, kh, ks); err != nil {
			return err
		}
	}
	for _, t := range pg.Tasks {
		if err := wr.exec("ins check", id, t.Line, t.Text, t.Done, t.Due, t.Box); err != nil {
			return err
		}
	}
	return nil
}

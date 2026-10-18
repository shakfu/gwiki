package wiki

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Change is a page and when it last changed.
type Change struct {
	PageInfo

	// Modified is the file's modification time.
	Modified time.Time `json:"modified"`

	// Author and Committed describe the last commit that touched the page;
	// both are empty when git has none, or when git is not available.
	Author    string    `json:"author,omitempty"`
	Committed time.Time `json:"committed,omitzero"`

	// Uncommitted marks a page with changes git has not recorded.
	Uncommitted bool `json:"uncommitted,omitempty"`
}

// Count is a name and how many pages it covers.
type Count struct {
	Name  string `json:"name"`
	Pages int    `json:"pages"`
}

// historyCommits bounds how far back Recent reads git history.
const historyCommits = 500

// Recent returns up to n pages, most recently modified first, with the last
// commit to each from git when the wiki is in a repository.
func (w *Wiki) Recent(n int) ([]Change, error) {
	rows, err := w.db.Query(`SELECT `+pageColumnsFrom("p")+`, f.mtime FROM pages p JOIN files f ON f.path = p.path || '.md'
		ORDER BY f.mtime DESC, p.path LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		var tags string
		var mtime int64
		if err := rows.Scan(&c.Path, &c.Title, &c.Type, &c.Status, &c.Priority, &c.Due, &tags, &mtime); err != nil {
			return nil, err
		}
		c.Tags = strings.Fields(tags)
		if c.Tags == nil {
			c.Tags = []string{}
		}
		c.Modified = time.Unix(0, mtime)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	commits, dirty := w.gitHistory()
	for i := range out {
		file := out[i].Path + ".md"
		if c, ok := commits[file]; ok {
			out[i].Author, out[i].Committed = c.author, c.when
		}
		out[i].Uncommitted = dirty[file]
	}
	return out, nil
}

type commit struct {
	author string
	when   time.Time
}

// gitHistory reads the last commit to each page, and the pages git reports
// as changed or untracked, keyed by path relative to the pages directory.
// Both are nil outside a repository or when git fails.
func (w *Wiki) gitHistory() (map[string]commit, map[string]bool) {
	if _, err := os.Stat(filepath.Join(w.Repo, ".git")); err != nil {
		return nil, nil
	}
	pages, err := filepath.Rel(w.Repo, w.PagesPath())
	if err != nil {
		return nil, nil
	}
	pages = filepath.ToSlash(pages)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	run := func(args ...string) ([]byte, bool) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", w.Repo}, args...)...)
		out, err := cmd.Output()
		return out, err == nil
	}
	rel := func(p string) (string, bool) {
		r, ok := strings.CutPrefix(p, pages+"/")
		return r, ok && strings.HasSuffix(r, ".md")
	}

	// -z leaves paths unquoted, so a page such as café.md matches its file.
	logOut, ok := run("log", "-z", "-n", strconv.Itoa(historyCommits), "--format=%x1e%ct%x1f%an", "--name-only", "--no-renames", "--", pages)
	if !ok {
		return nil, nil
	}
	commits := map[string]commit{}
	var cur commit
	afterHead := false
	for _, field := range strings.Split(string(logOut), "\x00") {
		if head, found := strings.CutPrefix(field, "\x1e"); found {
			secs, name, _ := strings.Cut(head, "\x1f")
			n, _ := strconv.ParseInt(secs, 10, 64)
			cur, afterHead = commit{author: name, when: time.Unix(n, 0)}, true
			continue
		}
		if afterHead {
			// A newline separates a commit's header from its first file.
			field, afterHead = strings.TrimPrefix(field, "\n"), false
		}
		if r, ok := rel(field); ok {
			if _, seen := commits[r]; !seen {
				commits[r] = cur
			}
		}
	}

	dirty := map[string]bool{}
	if statusOut, ok := run("status", "--porcelain", "-z", "--untracked-files=all", "--", pages); ok {
		fields := strings.Split(string(statusOut), "\x00")
		for i := 0; i < len(fields); i++ {
			entry := fields[i]
			if len(entry) < 4 {
				continue
			}
			if entry[0] == 'R' || entry[0] == 'C' {
				i++ // the source path follows as its own field
			}
			if r, ok := rel(entry[3:]); ok {
				dirty[r] = true
			}
		}
	}
	return commits, dirty
}

// DeadEnds returns pages that link to no other page.
func (w *Wiki) DeadEnds() ([]PageInfo, error) {
	rows, err := w.db.Query(`SELECT ` + pageColumns + ` FROM pages WHERE path NOT IN (
		SELECT page FROM links WHERE kind IN ('page', 'heading') AND resolved != page AND resolved != ''
	) ORDER BY path`)
	if err != nil {
		return nil, err
	}
	return scanPages(rows)
}

// TagCounts returns every tag with its page count, most used first.
func (w *Wiki) TagCounts() ([]Count, error) {
	return w.counts(`SELECT tag, count(DISTINCT page) FROM tags GROUP BY tag ORDER BY 2 DESC, 1`)
}

// Hubs returns up to n pages with the most other pages linking to them.
func (w *Wiki) Hubs(n int) ([]Count, error) {
	return w.counts(`SELECT resolved, count(DISTINCT page) FROM links
		WHERE kind IN ('page', 'heading') AND status IN ('ok', 'missing-heading') AND resolved != page
		GROUP BY resolved ORDER BY 2 DESC, 1 LIMIT ?`, n)
}

func (w *Wiki) counts(query string, args ...any) ([]Count, error) {
	rows, err := w.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Count{}
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Name, &c.Pages); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

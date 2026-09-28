// Package wiki is a project's markdown wiki: pages under .gwiki/wiki, and a
// derived SQLite cache of their titles, headings, links, tags and tasks.
//
// The pages are the truth. The cache is rebuilt from them whenever it is
// missing, stale or unreadable, and every query brings it up to date first.
// See docs/dev/wiki-design.md.
package wiki

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Layout, relative to the project root.
const (
	DirName   = ".gwiki"
	PagesDir  = "wiki"
	CacheFile = "cache.db"

	configFile = "config.json"

	// oldDirName is the directory before the project was renamed from gnotes.
	oldDirName = ".gnotes"
)

// gitignored are the .gwiki entries that are local to a clone.
var gitignored = []string{CacheFile + "*", "drafts/"}

// ErrNotFound reports that no wiki exists at or above a directory.
var ErrNotFound = errors.New("no gwiki found; run 'gwiki init'")

// Config is .gwiki/config.json.
type Config struct {
	Name string `json:"name"`
}

// Project is a located wiki.
type Project struct {
	// Root holds .gwiki.
	Root string

	// Repo is the top of the enclosing git working tree, or Root outside one.
	// File links resolve within it.
	Repo string

	// User marks the user wiki, ~/.gwiki, which only User opens.
	User bool

	Config Config
}

// Label names the wiki where the interface and the browser view show it:
// "user" for the user wiki, whatever its config says, else the project's name.
func (p *Project) Label() string {
	if p.User {
		return "user"
	}
	return p.Config.Name
}

// PagesPath returns the absolute pages directory.
func (p *Project) PagesPath() string { return filepath.Join(p.Root, DirName, PagesDir) }

// Cache returns the absolute cache path.
func (p *Project) Cache() string { return filepath.Join(p.Root, DirName, CacheFile) }

// Discover walks up from dir to the nearest directory holding .gwiki/wiki. It
// stops at the repository root, the first directory holding .git, and never
// takes the home directory for a project: its .gwiki is the user wiki, which
// only User opens.
func Discover(dir string) (*Project, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	old := ""
	for {
		if dir != home {
			if isDir(filepath.Join(dir, DirName, PagesDir)) {
				return load(dir, dir)
			}
			if isDir(filepath.Join(dir, oldDirName, PagesDir)) && old == "" {
				old = dir
			}
		}
		parent := filepath.Dir(dir)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil || parent == dir {
			if old != "" {
				return nil, fmt.Errorf("%w; %s has %s from before the rename to gwiki: rename it to %s, then run 'gwiki check' for links that name %s", ErrNotFound, old, oldDirName, DirName, oldDirName)
			}
			return nil, ErrNotFound
		}
		dir = parent
	}
}

// ErrNoUserWiki reports that ~/.gwiki holds no wiki.
var ErrNoUserWiki = errors.New("no user wiki; run 'gwiki -u init'")

// User locates the user wiki, ~/.gwiki. Its repository is found from ~/.gwiki
// upward, so ~/.gwiki/.git or a repository at ~ serves; with neither, file
// links stay within ~/.gwiki rather than reaching the whole home directory.
func User() (*Project, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if !isDir(filepath.Join(home, DirName, PagesDir)) {
		return nil, ErrNoUserWiki
	}
	p, err := load(home, filepath.Join(home, DirName))
	if err != nil {
		return nil, err
	}
	p.User = true
	return p, nil
}

// InitUser creates the user wiki, or completes a partial one.
func InitUser() (*Project, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if _, err := Init(home); err != nil {
		return nil, err
	}
	return User()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Init creates a wiki in dir, or completes a partial one. It keeps an existing
// config and .gitignore lines.
func Init(dir string) (*Project, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	gn := filepath.Join(dir, DirName)
	if err := os.MkdirAll(filepath.Join(gn, PagesDir), 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", filepath.Join(gn, PagesDir), err)
	}

	cfg := filepath.Join(gn, configFile)
	if _, err := os.Stat(cfg); os.IsNotExist(err) {
		raw, _ := json.MarshalIndent(Config{Name: filepath.Base(dir)}, "", "  ")
		if err := os.WriteFile(cfg, append(raw, '\n'), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", cfg, err)
		}
	}

	ignore := filepath.Join(gn, ".gitignore")
	raw, err := os.ReadFile(ignore)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", ignore, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	missing := ""
	for _, want := range gitignored {
		if !slices.Contains(lines, want) {
			missing += want + "\n"
		}
	}
	if missing != "" {
		if len(raw) > 0 && raw[len(raw)-1] != '\n' {
			missing = "\n" + missing
		}
		f, err := os.OpenFile(ignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("write %s: %w", ignore, err)
		}
		_, werr := f.WriteString(missing)
		if err := errors.Join(werr, f.Close()); err != nil {
			return nil, fmt.Errorf("write %s: %w", ignore, err)
		}
	}
	return load(dir, dir)
}

// load reads the project at root. Its repository is the first directory
// holding .git from repoFrom upward, else repoFrom.
func load(root, repoFrom string) (*Project, error) {
	p := &Project{Root: root, Repo: repoFrom}
	raw, err := os.ReadFile(filepath.Join(root, DirName, configFile))
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &p.Config); err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(root, DirName, configFile), err)
		}
	case !os.IsNotExist(err):
		return nil, err
	}
	if p.Config.Name == "" {
		p.Config.Name = filepath.Base(root)
	}
	for dir := repoFrom; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			p.Repo = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return p, nil
}

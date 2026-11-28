package wiki

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shakfu/gwiki/internal/markdown"
)

// A template is a page that a new page starts from: templates/<name>.md or
// .qmd in .gwiki, then in the per-user config directory, then built in. The
// first found wins, so a project or user can replace a built-in one. The new
// page takes the template's extension, its title, and today's date in a date
// key the template has.

//go:embed templates
var builtinTemplates embed.FS

// TemplatesDir is the directory under .gwiki holding the project's templates.
const TemplatesDir = "templates"

// Template is a template found by Templates.
type Template struct {
	Name   string `json:"name"`
	Ext    string `json:"ext"`
	Origin string `json:"origin"` // project, user or builtin
	File   string `json:"file,omitempty"`

	// Skipped says why the template cannot be used, as for a symlink. It
	// still hides a template of the same name further down the lookup.
	Skipped string `json:"skipped,omitempty"`
}

var templateName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// today is the date a template's date key is set to; a test replaces it.
var today = func() string { return time.Now().Format("2006-01-02") }

// UserTemplatesDir is the per-user templates directory, beside the per-user
// config.json.
func UserTemplatesDir() (string, error) {
	cfg, err := UserConfigPath(os.Getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), TemplatesDir), nil
}

type templateSource struct {
	origin string
	dir    string // empty for the built-in ones
	fsys   fs.FS
}

func (w *Wiki) templateSources() []templateSource {
	project := filepath.Join(w.Root, DirName, TemplatesDir)
	out := []templateSource{{"project", project, os.DirFS(project)}}
	if user, err := UserTemplatesDir(); err == nil {
		out = append(out, templateSource{"user", user, os.DirFS(user)})
	}
	sub, _ := fs.Sub(builtinTemplates, "templates")
	return append(out, templateSource{"builtin", "", sub})
}

// Templates lists every template by name, each from the first source that has
// it, skipped ones included. A source holding a.md and a.qmd is an error, since
// either could be meant.
func (w *Wiki) Templates() ([]Template, error) {
	seen := map[string]bool{}
	var out []Template
	for _, src := range w.templateSources() {
		entries, err := fs.ReadDir(src.fsys, ".")
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, fmt.Errorf("templates in %s: %w", src.dir, err)
		}
		here := map[string]string{}
		for _, e := range entries {
			name, ext := pageID(e.Name()), pageExt(e.Name())
			if ext == "" || !templateName.MatchString(name) {
				continue
			}
			if other, ok := here[name]; ok {
				return nil, fmt.Errorf("templates %s%s and %s%s in %s: remove one", name, other, name, ext, src.dir)
			}
			here[name] = ext
			if seen[name] {
				continue
			}
			seen[name] = true
			t := Template{Name: name, Ext: ext, Origin: src.origin}
			if src.dir != "" {
				t.File = filepath.Join(src.dir, name+ext)
			}
			// A symlink is skipped, not followed: a cloned repository's
			// template could otherwise copy any file of the reader's into a
			// page, ready to commit.
			if !e.Type().IsRegular() {
				t.Skipped = "not a regular file, and templates are not followed through symlinks"
			}
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b Template) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// template returns a template's extension and source.
func (w *Wiki) template(name string) (string, []byte, error) {
	if !templateName.MatchString(name) {
		return "", nil, fmt.Errorf("%q is not a template name: letters, digits, - and _", name)
	}
	all, err := w.Templates()
	if err != nil {
		return "", nil, err
	}
	var names []string
	for _, t := range all {
		if t.Name != name {
			if t.Skipped == "" {
				names = append(names, t.Name)
			}
			continue
		}
		// Not the next one of the name: that would fill the page with other
		// content than the one asked for.
		if t.Skipped != "" {
			return "", nil, fmt.Errorf("template %s is skipped: %s", t.File, t.Skipped)
		}
		var src []byte
		if t.File != "" {
			src, err = readRegular(t.File)
		} else {
			src, err = fs.ReadFile(builtinTemplates, path.Join("templates", name+t.Ext))
		}
		return t.Ext, src, err
	}
	return "", nil, fmt.Errorf("no template %q; there are %s", name, strings.Join(names, ", "))
}

// readRegular reads a file through its directory as an os.Root, refusing it
// unless it is a regular file, should it have become a symlink since it was
// listed.
func readRegular(file string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(file)
	if info, err := root.Lstat(name); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("template %s is not a regular file", file)
	}
	return root.ReadFile(name)
}

// quartoDates are date values quarto fills in at render time, kept as written.
var quartoDates = []string{"today", "now", "last-modified"}

// fromTemplate is a new page's source from a template. The title goes in its
// front matter title key; else in its first level-one heading, as "# Title";
// else in a new title key. A date key it has is set to today.
func fromTemplate(src []byte, title string, front func(*yaml.Node)) ([]byte, error) {
	pg := markdown.Parse(src)
	if pg.Front == nil || pg.Front.Raw["title"] == nil {
		if out, ok := retitle(src, pg, title); ok {
			return editTemplateFront(out, "", front)
		}
	}
	return editTemplateFront(src, title, front)
}

// retitle replaces the text of the first level-one heading, when it is an ATX
// heading, "# ...".
func retitle(src []byte, pg *markdown.Page, title string) ([]byte, bool) {
	for _, h := range pg.Headings {
		if h.Level != 1 {
			continue
		}
		lines := strings.SplitAfter(string(src), "\n")
		i := h.Line - 1
		if i >= len(lines) || !strings.HasPrefix(lines[i], "# ") {
			return nil, false
		}
		nl := lines[i][len(strings.TrimRight(lines[i], "\r\n")):]
		lines[i] = "# " + title + nl
		return []byte(strings.Join(lines, "")), true
	}
	return nil, false
}

// editTemplateFront sets title when it is not empty, dates the page, and
// applies front.
func editTemplateFront(src []byte, title string, front func(*yaml.Node)) ([]byte, error) {
	return editFront(src, func(m *yaml.Node) error {
		if title != "" {
			setScalar(m, "title", title)
		}
		// The value alone changes, so a date written plain stays plain.
		if v := mapValue(m, "date"); v != nil && v.Kind == yaml.ScalarNode && !slices.Contains(quartoDates, strings.TrimSpace(v.Value)) {
			v.Value = today()
		}
		front(m)
		return nil
	})
}

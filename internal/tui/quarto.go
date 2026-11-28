package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shakfu/gwiki/internal/display"
	"github.com/shakfu/gwiki/internal/quarto"
)

// quartoDoneMsg ends a render started by :quarto.
type quartoDoneMsg struct {
	job quarto.Job
	res quarto.Result
	err error
}

// quartoRender renders, in the background, the project of the directory
// selected in the tree when it holds a _quarto.yml, else the open page, as
// saved. The wiki is first exported to a staging directory, where quarto sees
// no hidden directory; see package quarto. One render runs at a time and
// quitting stops it.
func (m *WikiModel) quartoRender(arg string) error {
	format, err := quarto.ParseArgs(arg)
	if err != nil {
		return err
	}
	if m.rendering != nil {
		return errors.New("a render is running")
	}
	if m.edit != nil && m.edit.ed.Dirty {
		return errUnsaved
	}
	job := quarto.Job{Format: format, Out: m.w.RenderPath()}
	if m.focus == focusTree && m.treeCursor < len(m.rows) && m.rows[m.treeCursor].dir != "" {
		dir := m.rows[m.treeCursor].dir
		if _, err := os.Stat(filepath.Join(m.w.PagesPath(), filepath.FromSlash(dir), quarto.Project)); err != nil {
			return fmt.Errorf("%s has no %s; open a page to render it alone", dir, quarto.Project)
		}
		job.Dir = dir
	} else {
		if m.cur == nil {
			return errors.New(":quarto needs an open page, or a directory with " + quarto.Project + " selected in the tree")
		}
		rel, err := filepath.Rel(m.w.PagesPath(), m.w.PageFile(m.cur.info.Path))
		if err != nil {
			return err
		}
		job.File = filepath.ToSlash(rel)
	}

	if job.Stage, err = quarto.StageDir(m.w.PagesPath()); err != nil {
		return err
	}
	exp, err := m.w.Export(job.Stage)
	if err != nil {
		return fmt.Errorf("stage the wiki in %s: %w", job.Stage, err)
	}
	job.Inputs = map[string]bool{}
	for _, rel := range exp.Written {
		job.Inputs[rel] = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.rendering = cancel
	m.after = func() tea.Msg {
		res, err := quarto.Render(ctx, job)
		return quartoDoneMsg{job: job, res: res, err: err}
	}
	m.setStatus("rendering " + job.File + job.Dir + "...")
	return nil
}

func (m *WikiModel) quartoDone(msg quartoDoneMsg) {
	m.rendering()
	m.rendering = nil
	if msg.err != nil {
		m.setError(msg.err)
		return
	}
	out := m.w.RenderPath()
	if rel, err := filepath.Rel(m.w.Root, out); err == nil {
		out = filepath.ToSlash(rel)
	}
	written := msg.res.Written
	if len(written) == 0 {
		m.setError(errors.New("quarto render wrote nothing"))
		return
	}
	if msg.job.File != "" {
		// The page's own outputs, not its _files support directory.
		stem := strings.TrimSuffix(path.Base(msg.job.File), path.Ext(msg.job.File)) + "."
		var named []string
		for _, rel := range written {
			if strings.HasPrefix(path.Base(rel), stem) {
				named = append(named, rel)
			}
		}
		if len(named) > 0 {
			m.setStatus("rendered " + strings.Join(named, ", ") + " in " + out)
			return
		}
	}
	m.setStatus("rendered " + msg.job.File + msg.job.Dir + ": " + display.Plural(len(written), "file") + " in " + out)
}

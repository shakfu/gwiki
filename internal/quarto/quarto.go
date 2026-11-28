// Package quarto runs `quarto render` on a staged copy of the wiki.
//
// Quarto leaves out every input whose path has a hidden directory in it, so a
// page under .gwiki/wiki is never part of a project, and a project there
// renders nothing. Rendering therefore runs in a copy of the pages directory,
// exported to a staging directory with no hidden component, which is kept so
// quarto's caches last between renders. What the render writes is copied to
// .gwiki/render at the same relative path. Rendering runs the page's code
// cells; it is started only by a person's command.
package quarto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ErrMissing reports that no quarto executable is in PATH.
var ErrMissing = errors.New("quarto is not in PATH")

// Usage is the command's syntax, for errors and help.
const Usage = "render [--to format | to=format]"

// Project is the file that makes a directory a Quarto project.
const Project = "_quarto.yml"

// ParseArgs reads the arguments of `:quarto`, returning the format, or "" for
// every format the front matter or project lists.
func ParseArgs(arg string) (string, error) {
	fields := strings.Fields(arg)
	if len(fields) == 0 || fields[0] != "render" {
		return "", fmt.Errorf("usage: :quarto %s", Usage)
	}
	var to string
	switch rest := fields[1:]; {
	case len(rest) == 0:
	case len(rest) == 2 && rest[0] == "--to":
		to = rest[1]
	case len(rest) == 1 && strings.HasPrefix(rest[0], "--to="):
		to = strings.TrimPrefix(rest[0], "--to=")
	case len(rest) == 1 && strings.HasPrefix(rest[0], "to="):
		to = strings.TrimPrefix(rest[0], "to=")
	default:
		return "", fmt.Errorf("usage: :quarto %s", Usage)
	}
	// Passed as one argument, never through a shell; a leading - would be
	// read as another flag.
	if len(fields) > 1 && (to == "" || strings.HasPrefix(to, "-")) {
		return "", fmt.Errorf("usage: :quarto %s", Usage)
	}
	return to, nil
}

// StageDir is the staging directory for the pages directory pages: under the
// system temporary directory, in a directory only this user can enter. A path
// with a hidden component, where quarto would render nothing, is an error.
func StageDir(pages string) (string, error) {
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", err
	}
	user := filepath.Join(base, "gwiki-"+userID())
	if err := os.Mkdir(user, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", err
	}
	if err := private(user); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(pages))
	// Named for the directory holding .gwiki, for a person looking, less any
	// leading dot, as in ~/.dotfiles, which would hide the copy from quarto.
	name := strings.TrimLeft(filepath.Base(filepath.Dir(filepath.Dir(pages))), ".")
	dir := filepath.Join(user, name+"-"+hex.EncodeToString(sum[:6]))
	if hidden(dir) {
		return "", fmt.Errorf("quarto skips files under hidden directories, and the staging directory %s is under one; set TMPDIR to another directory", dir)
	}
	return dir, nil
}

// hidden reports whether a path has a component starting with a dot.
func hidden(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

// Job is one render in a staging directory.
type Job struct {
	// Stage is the staging directory. Inputs are the files the export wrote
	// there, slash-separated relative to it.
	Stage  string
	Inputs map[string]bool

	// File is the page to render, relative to Stage; when it is empty, Dir
	// is the project to render.
	File, Dir string

	// Format is passed as --to; empty renders every listed format.
	Format string

	// Out receives what the render wrote, at its path relative to Stage.
	Out string
}

// Result is a finished render.
type Result struct {
	// Written are the files the render created or changed, and Removed those
	// it deleted, relative to Out and sorted.
	Written, Removed []string

	// Log is quarto's combined output.
	Log []byte
}

// Render runs `quarto render <file> [--to format]` in the file's directory, or
// `quarto render [--to format]` in the project's, then mirrors its outputs to
// Out. Out is created with a .gitignore that ignores it. A failure carries the
// last line quarto printed and keeps the whole log in Out.
func Render(ctx context.Context, job Job) (Result, error) {
	bin, err := exec.LookPath("quarto")
	if err != nil {
		return Result{}, ErrMissing
	}
	if err := ignored(job.Out); err != nil {
		return Result{}, err
	}
	args, dir, name := []string{"render"}, job.Dir, path.Base(job.Dir)
	if job.File != "" {
		args, dir, name = append(args, path.Base(job.File)), path.Dir(job.File), path.Base(job.File)
	}
	if job.Format != "" {
		args = append(args, "--to", job.Format)
	}
	before, err := snapshot(job.Stage)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = filepath.Join(job.Stage, filepath.FromSlash(dir))
	log, err := cmd.CombinedOutput()
	res := Result{Log: log, Written: []string{}, Removed: []string{}}
	if err != nil {
		if ctx.Err() != nil {
			return res, fmt.Errorf("quarto render stopped: %w", ctx.Err())
		}
		if last := lastLine(log); last != "" {
			err = errors.New(last)
		}
		logFile := filepath.Join(job.Out, name+".log")
		if werr := os.WriteFile(logFile, log, 0o644); werr != nil {
			return res, fmt.Errorf("quarto render: %w (log not kept: %v)", err, werr)
		}
		return res, fmt.Errorf("quarto render: %w; log in %s", err, logFile)
	}
	after, err := snapshot(job.Stage)
	if err != nil {
		return res, err
	}
	for rel, st := range after {
		if was, ok := before[rel]; (!ok || was != st) && output(job, rel) {
			res.Written = append(res.Written, rel)
		}
	}
	for rel := range before {
		if _, ok := after[rel]; !ok && output(job, rel) {
			res.Removed = append(res.Removed, rel)
		}
	}
	slices.Sort(res.Written)
	slices.Sort(res.Removed)
	return res, mirror(job, res)
}

// output reports whether a staged file is the render's: not an exported input,
// and not in quarto's caches, .quarto and _freeze.
func output(job Job, rel string) bool {
	if job.Inputs[rel] || hidden(rel) {
		return false
	}
	return !slices.Contains(strings.Split(rel, "/"), "_freeze")
}

// mirror copies what the render wrote to Out and removes what it deleted.
func mirror(job Job, res Result) error {
	for _, rel := range res.Written {
		data, err := os.ReadFile(filepath.Join(job.Stage, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		dst := filepath.Join(job.Out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	for _, rel := range res.Removed {
		if err := os.Remove(filepath.Join(job.Out, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

type stat struct {
	size  int64
	mtime int64
}

// snapshot is every regular file under dir with its size and modification
// time, by slash-separated path.
func snapshot(dir string) (map[string]stat, error) {
	out := map[string]stat{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = stat{info.Size(), info.ModTime().UnixNano()}
		return nil
	})
	return out, err
}

// ignored creates root with a .gitignore that ignores everything in it, so
// output is never committed.
func ignored(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	ignore := filepath.Join(root, ".gitignore")
	_, err := os.Stat(ignore)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(ignore, []byte("*\n"), 0o644)
	}
	return err
}

func lastLine(b []byte) string {
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	return strings.TrimSpace(string(lines[len(lines)-1]))
}

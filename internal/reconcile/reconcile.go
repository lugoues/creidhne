// Package reconcile diffs a desired set of generated quadlet files against a
// live quadlet directory and applies the difference.
//
// It performs plain filesystem operations with whatever privileges the process
// was given. It never escalates privileges itself. Writing to a root-owned
// directory (e.g. /etc/containers/systemd) is the caller's responsibility
// (`sudo crei apply ...`). It preserves the safety-critical invariant of the
// original prototype: only regular files with known extensions (plus the
// images/ subtree) are ever considered for removal, never directories.
package reconcile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/pmezard/go-difflib/difflib"
)

// quadletExts are the managed unit-file extensions. Flat files outside images/
// must match one of these to be considered ours.
var quadletExts = map[string]bool{
	".container": true, ".pod": true, ".volume": true, ".network": true,
	".kube": true, ".build": true, ".image": true, ".artifact": true,
}

// DesiredFile is a generated file's content and optional octal mode (e.g.
// "0755" for an executable build-context script; empty means default perms).
type DesiredFile struct {
	Content []byte
	Mode    string
}

// ActionKind classifies a planned change.
type ActionKind int

const (
	ActionAdd ActionKind = iota
	ActionChange
	ActionUnchanged
	ActionRemove
)

// Change is a single planned filesystem change.
type Change struct {
	Action   ActionKind
	Name     string // path relative to the quadlet dir (slash-separated)
	Content  []byte // add, change
	Mode     string // add, change
	Existing []byte // change (for diffing)
}

// Summary counts a plan. Total intentionally excludes removals, matching the
// prototype's planSummary.
type Summary struct {
	Added, Changed, Unchanged, Removed, Total int
}

// ComputePlan compares desired files against what is on disk under dir and
// returns changes ordered as: desired files (sorted), then extra on-disk files
// to remove (sorted).
func ComputePlan(desired map[string]DesiredFile, dir string) ([]Change, error) {
	var changes []Change

	names := make([]string, 0, len(desired))
	for n := range desired {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		fc := desired[name]
		dest := filepath.Join(dir, filepath.FromSlash(name))
		existing, err := os.ReadFile(dest)
		switch {
		case os.IsNotExist(err):
			changes = append(changes, Change{Action: ActionAdd, Name: name, Content: fc.Content, Mode: fc.Mode})
		case err != nil:
			return nil, err
		case !bytes.Equal(existing, fc.Content):
			changes = append(changes, Change{Action: ActionChange, Name: name, Content: fc.Content, Mode: fc.Mode, Existing: existing})
		default:
			changes = append(changes, Change{Action: ActionUnchanged, Name: name})
		}
	}

	onDisk, err := ListExisting(dir)
	if err != nil {
		return nil, err
	}
	var extra []string
	for _, name := range onDisk {
		if _, ok := desired[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		changes = append(changes, Change{Action: ActionRemove, Name: name})
	}
	return changes, nil
}

// Summarize counts actions in a plan.
func Summarize(changes []Change) Summary {
	var s Summary
	for _, c := range changes {
		switch c.Action {
		case ActionAdd:
			s.Added++
		case ActionChange:
			s.Changed++
		case ActionUnchanged:
			s.Unchanged++
		case ActionRemove:
			s.Removed++
		}
	}
	s.Total = s.Added + s.Changed + s.Unchanged
	return s
}

// ListExisting returns the managed files under dir: flat files with a quadlet
// extension, plus every file in the images/ subtree (recursively). It never
// returns a directory — the property that prevents a stale directory from being
// scheduled for removal.
func ListExisting(dir string) ([]string, error) {
	var out []string
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if quadletExts[filepath.Ext(e.Name())] {
			out = append(out, e.Name())
		}
	}
	imagesDir := filepath.Join(dir, "images")
	if fi, err := os.Stat(imagesDir); err == nil && fi.IsDir() {
		err := filepath.WalkDir(imagesDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Mkdirp creates dir and parents.
func Mkdirp(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// RemoveFile removes a file or directory tree.
func RemoveFile(path string) error {
	return os.RemoveAll(path)
}

// ensureDir guarantees dir exists, clearing any stale file blocking the path
// (the file->directory transition the prototype handles).
func ensureDir(dir string) error {
	fi, err := os.Stat(dir)
	if err == nil {
		if fi.IsDir() {
			return nil
		}
		if err := RemoveFile(dir); err != nil {
			return err
		}
		return Mkdirp(dir)
	}
	if !os.IsNotExist(err) {
		return err
	}
	if parent := filepath.Dir(dir); parent != dir {
		if err := ensureDir(parent); err != nil {
			return err
		}
	}
	return Mkdirp(dir)
}

// WriteFile writes content to path (creating parents) and applies mode if set.
func WriteFile(path string, content []byte, mode string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return err
	}
	if mode != "" {
		m, err := parseMode(mode)
		if err != nil {
			return err
		}
		return os.Chmod(path, m)
	}
	return nil
}

// PruneEmptyDirs removes empty directories under dir bottom-up, never removing
// dir itself. Used to clean up images/ after removals.
func PruneEmptyDirs(dir string) error {
	fi, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return nil
	}
	var walk func(p string) error
	walk = func(p string) error {
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				if err := walk(filepath.Join(p, e.Name())); err != nil {
					return err
				}
			}
		}
		if p == dir {
			return nil
		}
		entries, err = os.ReadDir(p)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return os.Remove(p)
		}
		return nil
	}
	return walk(dir)
}

// RunDiff returns a diff between the live file and newContent. The default tool
// ("diff" or "") uses a built-in unified differ so the binary stays
// self-contained; any other tool name is invoked as `tool <live> <tmp>`.
func RunDiff(livePath string, newContent []byte, liveLabel, newLabel, tool string) (string, error) {
	if tool == "" || tool == "diff" {
		existing, err := os.ReadFile(livePath)
		if err != nil {
			return "", err
		}
		return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(string(existing)),
			B:        difflib.SplitLines(string(newContent)),
			FromFile: liveLabel,
			ToFile:   newLabel,
			Context:  3,
		})
	}
	tmp, err := os.CreateTemp("", "quadlet-new-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(newContent); err != nil {
		_ = tmp.Close()
		return "", err
	}
	_ = tmp.Close()
	out, _ := exec.Command(tool, livePath, tmp.Name()).Output()
	return string(out), nil
}

// DaemonReload runs `systemctl daemon-reload`, scoped to --user when userScope
// is true (a rootless quadlet dir under $HOME). It does not use sudo: a system
// reload requires the caller to already be root (`sudo crei apply`).
func DaemonReload(userScope bool) error {
	if userScope {
		return run("systemctl", "--user", "daemon-reload")
	}
	return run("systemctl", "daemon-reload")
}

// ReloadHint is the command string to suggest when --reload was not passed.
func ReloadHint(userScope bool) string {
	if userScope {
		return "systemctl --user daemon-reload"
	}
	return "systemctl daemon-reload"
}

func parseMode(s string) (os.FileMode, error) {
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q: %w", s, err)
	}
	return os.FileMode(v), nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

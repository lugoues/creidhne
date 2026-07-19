// Package state records what crei last applied: the evaluated manifest plus
// the hash of every file actually written, per quadlet. It is the "recorded"
// layer between the CUE desired state and the files on disk (the kubectl
// last-applied-configuration analogue): status reads it to distinguish a
// pending edit (desired != recorded) from tampering (recorded != disk) and to
// group/label units without needing a working CUE eval.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lugoues/creidhne/internal/eval"
)

// Filename is the state file's name inside the quadlet directory. Its
// extension is not a managed quadlet extension, so reconcile.ListExisting
// never sees it and quadlet itself ignores it.
const Filename = "crei.state"

// Version is the current schema version of the state file.
const Version = 1

// Unit mirrors eval.UnitRecord with the manifest's lowercase field names, so
// the state file reads like the CUE-side manifest it snapshots.
type Unit struct {
	Kind     string         `json:"kind"`
	Stem     string         `json:"stem"`
	Filename string         `json:"filename"`
	Service  string         `json:"service,omitempty"`
	Data     map[string]any `json:"data"`
}

// File records one file crei wrote: its quadlet-dir-relative path, the SHA-256
// of the bytes written, the mode, and when its content last changed via apply
// (carried forward across applies that leave it untouched). Top-level unit
// files also retain their exact content plus superseded versions, so the
// config a still-running unit was created from stays diffable after later
// applies (staleness diffs); images/ artifacts carry neither.
type File struct {
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	Mode      string    `json:"mode"`
	AppliedAt time.Time `json:"appliedAt"`
	Content   string    `json:"content,omitempty"`
	History   []Revision `json:"history,omitempty"`
}

// Revision is one superseded applied content of a file, oldest first.
type Revision struct {
	SHA256    string    `json:"sha256"`
	AppliedAt time.Time `json:"appliedAt"`
	Content   string    `json:"content"`
}

// historyCap bounds retained versions per file; a unit left running through
// more content changes than this loses its diff, never its staleness flag.
const historyCap = 10

// InEffectAt returns the file content that was applied and current at t: the
// newest version (history or present) whose AppliedAt is not after t. ok is
// false when that version predates retention (or content was never recorded).
func (f File) InEffectAt(t time.Time) (content string, ok bool) {
	if !f.AppliedAt.After(t) {
		return f.Content, f.Content != ""
	}
	for i := len(f.History) - 1; i >= 0; i-- {
		if !f.History[i].AppliedAt.After(t) {
			return f.History[i].Content, f.History[i].Content != ""
		}
	}
	return "", false
}

// Quadlet is one quadlet's record: the manifest as evaluated at apply time and
// the files that render produced for it.
type Quadlet struct {
	AppliedAt time.Time `json:"appliedAt"`
	Units     []Unit    `json:"units"`
	Files     []File    `json:"files"`
}

// State is the whole crei.state document.
type State struct {
	Version     int                `json:"version"`
	CreiVersion string             `json:"creiVersion"`
	Quadlets    map[string]Quadlet `json:"quadlets"`
}

// FileInput is a rendered file's content and mode, pre-hash.
type FileInput struct {
	Content []byte
	Mode    string
}

// Path returns the state file location for a quadlet directory.
func Path(quadletDir string) string {
	return filepath.Join(quadletDir, Filename)
}

// Load reads the state file for a quadlet directory. A missing file returns
// (nil, nil): recorded state simply doesn't exist yet.
func Load(quadletDir string) (*State, error) {
	raw, err := os.ReadFile(Path(quadletDir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Filename, err)
	}
	return &s, nil
}

// Write atomically replaces the state file (temp file + rename in the same
// directory, so a crash never leaves a torn document).
func Write(quadletDir string, s *State) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(quadletDir, Filename+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Cleanup on any failure is best-effort: the write error is what matters.
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, Path(quadletDir)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// Build assembles a new State from the evaluated quadlets and their rendered
// files. AppliedAt on a file carries forward from prev when its content hash
// is unchanged, so "when did this file last actually change" survives no-op
// applies; a new or changed file gets now.
func Build(creiVersion string, now time.Time, prev *State, quads []eval.Quadlet, filesByQuadlet map[string]map[string]FileInput) *State {
	s := &State{Version: Version, CreiVersion: creiVersion, Quadlets: map[string]Quadlet{}}
	for _, q := range quads {
		rec := Quadlet{AppliedAt: now}
		for _, u := range q.Units {
			rec.Units = append(rec.Units, Unit{
				Kind: u.Kind, Stem: u.Stem, Filename: u.Filename, Service: u.Service, Data: u.Data,
			})
		}
		var prevFiles map[string]File
		if prev != nil {
			if pq, ok := prev.Quadlets[q.Name]; ok {
				prevFiles = make(map[string]File, len(pq.Files))
				for _, f := range pq.Files {
					prevFiles[f.Path] = f
				}
			}
		}
		paths := make([]string, 0, len(filesByQuadlet[q.Name]))
		for p := range filesByQuadlet[q.Name] {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			in := filesByQuadlet[q.Name][p]
			f := File{Path: p, SHA256: HashBytes(in.Content), Mode: in.Mode, AppliedAt: now}
			// Content (and history) only for unit files, which are always
			// top-level; images/ artifacts can be binary and no service runs
			// from them, so they have no staleness to diff.
			if !strings.Contains(p, "/") {
				f.Content = string(in.Content)
			}
			if pf, ok := prevFiles[p]; ok {
				if pf.SHA256 == f.SHA256 {
					f.AppliedAt = pf.AppliedAt
					f.History = pf.History
				} else {
					f.History = pf.History
					// Pre-history records carry no content; nothing to retain.
					if pf.Content != "" {
						f.History = append(append([]Revision{}, pf.History...), Revision{
							SHA256: pf.SHA256, AppliedAt: pf.AppliedAt, Content: pf.Content,
						})
					}
					if n := len(f.History) - historyCap; n > 0 {
						f.History = f.History[n:]
					}
				}
			}
			rec.Files = append(rec.Files, f)
		}
		s.Quadlets[q.Name] = rec
	}
	return s
}

// FileOwner returns, for every recorded file path, the quadlet that owns it.
func (s *State) FileOwner() map[string]string {
	if s == nil {
		return nil
	}
	owner := map[string]string{}
	for name, q := range s.Quadlets {
		for _, f := range q.Files {
			owner[f.Path] = name
		}
	}
	return owner
}

// FileRecord looks up a recorded file by its quadlet-dir-relative path.
func (s *State) FileRecord(path string) (File, bool) {
	if s == nil {
		return File{}, false
	}
	for _, q := range s.Quadlets {
		for _, f := range q.Files {
			if f.Path == path {
				return f, true
			}
		}
	}
	return File{}, false
}

// HashBytes is the state file's content hash: hex SHA-256.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Equal reports whether two states describe the same recorded content,
// ignoring timestamps and crei version. Used to skip rewriting an unchanged
// state file (e.g. a "nothing to do" apply against a read-only dir). It
// compares normalized marshaled forms so it cannot drift as fields are added.
func Equal(a, b *State) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if a == nil {
		return true
	}
	return string(normalized(a)) == string(normalized(b))
}

// normalized marshals a copy of s with every timestamp and the crei version
// zeroed, so Equal sees only recorded content.
func normalized(s *State) []byte {
	c := State{Version: s.Version, Quadlets: map[string]Quadlet{}}
	for name, q := range s.Quadlets {
		qc := Quadlet{Units: q.Units}
		for _, f := range q.Files {
			f.AppliedAt = time.Time{}
			qc.Files = append(qc.Files, f)
		}
		c.Quadlets[name] = qc
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil
	}
	return raw
}

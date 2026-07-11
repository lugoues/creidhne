package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/state"
)

// recordState writes crei.state after an apply: the evaluated manifest plus
// per-file hashes of what render produced, with AppliedAt carried forward for
// files whose content didn't change. strict controls how a write failure is
// treated: a real apply just wrote files so it must also be able to record
// them (error); a "nothing to do" apply only adopts state opportunistically,
// so a read-only dir downgrades to a warning.
func recordState(out io.Writer, quadletDir string, quads []eval.Quadlet, strict bool) error {
	prev, err := state.Load(quadletDir)
	if err != nil {
		// A corrupt state file self-heals: warn and rebuild from scratch.
		fmt.Fprintf(out, "warning: ignoring unreadable state: %v\n", err)
		prev = nil
	}
	filesByQuadlet, err := perQuadletFiles(quads)
	if err != nil {
		return err
	}
	next := state.Build(version, time.Now(), prev, quads, filesByQuadlet)
	if state.Equal(prev, next) {
		return nil
	}
	if err := state.Write(quadletDir, next); err != nil {
		if !strict {
			fmt.Fprintf(out, "warning: could not record state: %v\n", err)
			return nil
		}
		return fmt.Errorf("record state: %w", err)
	}
	return nil
}

// perQuadletFiles renders each quadlet on its own (subset rendering is valid:
// cross-quadlet refs resolve at eval time) so every produced file is
// attributable to the quadlet that owns it.
func perQuadletFiles(quads []eval.Quadlet) (map[string]map[string]state.FileInput, error) {
	filesByQuadlet := make(map[string]map[string]state.FileInput, len(quads))
	for _, q := range quads {
		fs, err := renderQuadlets([]eval.Quadlet{q})
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", q.Name, err)
		}
		in := make(map[string]state.FileInput, len(fs))
		for p, f := range fs {
			in[p] = state.FileInput{Content: f.Content, Mode: f.Mode}
		}
		filesByQuadlet[q.Name] = in
	}
	return filesByQuadlet, nil
}

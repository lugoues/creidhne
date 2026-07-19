package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/lugoues/creidhne/internal/state"
)

// restartHint is non-empty when restarting the unit cannot apply its pending
// change: the created object outlives the unit, so the oneshot re-run is a
// no-op against it.
func restartHint(path string, rec state.File) string {
	switch {
	case strings.HasSuffix(path, ".volume"):
		// quadlet volume units run `podman volume create --ignore`.
		return path + ": restart will not apply this change (an existing volume is never modified); recreate the volume to apply it"
	case strings.HasSuffix(path, ".network") && !strings.Contains(rec.Content, "NetworkDeleteOnStop=true"):
		return path + ": restart will not apply this change (the network object outlives the unit without NetworkDeleteOnStop); recreate the network to apply it"
	}
	return ""
}

// staleNote compacts what a unit's staleness means for the status table: a
// "recreate required" flag for kinds a restart cannot fix, and the changed
// keys when history still holds the running config.
func staleNote(path string, rec state.File, activeEnter time.Time) string {
	var parts []string
	if restartHint(path, rec) != "" {
		parts = append(parts, "recreate required")
	}
	if old, ok := rec.InEffectAt(activeEnter); ok {
		if keys := changedKeys(old, rec.Content); len(keys) > 0 {
			const max = 3
			if len(keys) > max {
				keys = append(keys[:max], fmt.Sprintf("+%d more", len(keys)-max))
			}
			parts = append(parts, strings.Join(keys, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

// changedKeys diffs two unit files at key granularity: keys whose value
// sequence differs between the in-effect and current content. Compared
// section-qualified, displayed bare.
func changedKeys(old, current string) []string {
	po, pc := unitKeyValues(old), unitKeyValues(current)
	union := map[string]bool{}
	for k := range po {
		union[k] = true
	}
	for k := range pc {
		union[k] = true
	}
	qualified := make([]string, 0, len(union))
	for k := range union {
		if strings.Join(po[k], "\x00") != strings.Join(pc[k], "\x00") {
			qualified = append(qualified, k)
		}
	}
	sort.Strings(qualified)
	var out []string
	seen := map[string]bool{}
	for _, q := range qualified {
		k := q
		if i := strings.IndexByte(q, '.'); i >= 0 {
			k = q[i+1:]
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// unitKeyValues parses systemd-INI unit content into section.key -> values in
// file order (repeated keys are lists in quadlet).
func unitKeyValues(content string) map[string][]string {
	m := map[string][]string{}
	section := ""
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := section + "." + strings.TrimSpace(k)
		m[key] = append(m[key], strings.TrimSpace(v))
	}
	return m
}

// diffStale prints, per stale unit, the diff between the config the running
// process was created from (state history) and the currently applied file.
func diffStale(out io.Writer, cfg config) error {
	in, notes, err := gatherStatus(cfg, nil)
	if err != nil {
		return err
	}
	for _, n := range notes {
		fmt.Fprintln(out, yellow("! "+n))
	}
	var stale []statusRow
	for _, r := range classifyRows(in) {
		if r.Stale {
			stale = append(stale, r)
		}
	}
	if len(stale) == 0 {
		fmt.Fprintln(out, "No stale units.")
		return nil
	}
	for i, r := range stale {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, diffHeaderStyle.Render("# "+r.Path))
		rec, _ := in.Recorded.FileRecord(r.Path)
		if hint := restartHint(r.Path, rec); hint != "" {
			fmt.Fprintln(out, yellow("! "+hint))
		}
		old, ok := rec.InEffectAt(in.Runtime[r.Service].ActiveEnter)
		switch {
		case !ok:
			fmt.Fprintln(out, dim("  running config predates recorded history; no diff available (history starts with the first apply on crei >= this version)"))
		case old == rec.Content:
			fmt.Fprintln(out, dim("  no content difference recorded"))
		default:
			renderInlineDiff(out, []byte(old), []byte(rec.Content), cfg.DiffStyle)
		}
	}
	fmt.Fprintf(out, "\n%d stale unit(s)\n", len(stale))
	return nil
}

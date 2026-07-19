package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lugoues/creidhne/internal/eval"
	"github.com/lugoues/creidhne/internal/reconcile"
	"github.com/lugoues/creidhne/internal/state"
	"github.com/lugoues/creidhne/internal/systemd"
)

// Disk-state labels for the status table. See the classification matrix in
// classifyRows: desired (CUE) vs recorded (crei.state) vs actual (disk).
const (
	diskSynced   = "synced"   // desired == disk
	diskPending  = "pending"  // desired != disk, disk matches last apply
	diskTampered = "tampered" // disk != recorded: changed outside crei
	diskMissing  = "missing"  // desired/recorded, not on disk
	diskOrphan   = "orphan"   // on disk + recorded, no longer desired
	diskForeign  = "foreign"  // on disk, never recorded: not crei's
	diskApplied  = "applied"  // matches last apply (desired unknown: eval failed)
	diskUnknown  = "unknown"  // no desired and no recorded state to judge by
)

// statusRow is one line of the table.
type statusRow struct {
	Quadlet  string
	Path     string
	Service  string // "" for artifacts (images/...) and unit kinds with no service
	Disk     string
	Loaded   string // "", "ok", "reload needed", "not loaded"
	Runtime  string // "", "running", "failed", ...
	Since    time.Duration
	Stale    bool // running, but started before this file's last content change
	artifact bool
}

// statusInput is everything classifyRows needs, gathered by the command.
type statusInput struct {
	// desired: per-quadlet rendered files; nil when eval failed.
	Desired map[string]map[string]state.FileInput
	// units from the eval manifest, when available.
	DesiredUnits []eval.Quadlet
	// recorded state; nil when absent.
	Recorded *state.State
	// disk: relative path -> content hash.
	Disk map[string]string
	// runtime: service -> status; nil when systemctl is unavailable.
	Runtime map[string]systemd.UnitStatus
	Now     time.Time
}

func newStatusCmd() *cobra.Command {
	var check, problems bool
	var format string
	cmd := &cobra.Command{
		Use:   "status [quadlet...]",
		Short: "Show desired vs recorded vs disk vs runtime state per unit",
		Long: "status compares four layers per unit and prints one table, grouped by\n" +
			"quadlet:\n\n" +
			"  DISK     the CUE desired state vs crei.state (what apply last wrote)\n" +
			"           vs the files on disk: synced, pending (your edit, run apply),\n" +
			"           tampered (changed outside crei), missing, orphan, foreign\n" +
			"  LOADED   whether systemd's generator has picked the file up\n" +
			"           (not loaded / reload needed / ok)\n" +
			"  RUNTIME  the service's ActiveState, how long it has been up, and\n" +
			"           (stale) when the running process predates the last apply\n\n" +
			"Every layer degrades independently: a broken CUE eval falls back to\n" +
			"recorded state, no crei.state falls back to desired vs disk, and a\n" +
			"missing systemd leaves the runtime columns blank. Read-only.\n\n" +
			"With no arguments it shows the whole deployment, hiding synced images/\n" +
			"build files (they carry no runtime state); given quadlet names it shows\n" +
			"only those, including every file. Names match desired quadlets or ones\n" +
			"still recorded in crei.state (so a removed quadlet's leftovers are\n" +
			"inspectable by name).\n\n" +
			"--check exits non-zero unless everything is synced, loaded, and healthy\n" +
			"(for cron/CI).",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := resolveConfig()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			in, notes, err := gatherStatus(cfg, args)
			if err != nil {
				return err
			}
			rows := classifyRows(in)
			if problems {
				rows = filterProblems(rows)
			}
			switch format {
			case "table":
				renderStatus(out, cfg.QuadletDir, rows, notes, in, renderOpts{
					ShowAllArtifacts: len(args) > 0 || problems,
					ProblemsOnly:     problems,
				})
			case "json":
				if err := renderStatusJSON(out, cfg.QuadletDir, rows, notes); err != nil {
					return err
				}
			default:
				return fmt.Errorf("invalid --format %q (want table or json)", format)
			}
			if check && !statusClean(rows, notes) {
				return errSilent{}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero unless fully synced, loaded, and healthy")
	cmd.Flags().BoolVar(&problems, "problems", false, "show only rows needing attention (not synced, reload needed, failed, stale, activating)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table or json (json always includes artifact rows)")
	return cmd
}

// filterProblems keeps rows needing attention. inactive is not a problem (a
// stopped service is often intentional); activating is (either transient or a
// unit stuck in a start loop, both worth eyes).
func filterProblems(rows []statusRow) []statusRow {
	out := rows[:0:0]
	for _, r := range rows {
		if rowIsProblem(r) {
			out = append(out, r)
		}
	}
	return out
}

func rowIsProblem(r statusRow) bool {
	if r.Disk != diskSynced && r.Disk != diskApplied {
		return true
	}
	if r.Loaded == "reload needed" || r.Loaded == "not loaded" {
		return true
	}
	return r.Runtime == "failed" || r.Runtime == "activating" || r.Stale
}

// gatherStatus collects the four layers, degrading each independently: any
// layer that cannot be read becomes a note instead of an error. only, when
// non-empty, restricts the view to those quadlets; names are validated against
// the union of desired and recorded quadlets, so a quadlet that survives only
// in crei.state is still addressable.
func gatherStatus(cfg config, only []string) (statusInput, []string, error) {
	in := statusInput{Now: time.Now()}
	var notes []string

	if quads, err := loadQuadlets(cfg.ProjectDir); err != nil {
		notes = append(notes, "eval failed: "+errHead(err)+" (showing recorded/disk state)")
	} else if files, err := perQuadletFiles(quads); err != nil {
		notes = append(notes, "render failed: "+errHead(err)+" (showing recorded/disk state)")
	} else {
		in.Desired = files
		in.DesiredUnits = quads
	}

	if st, err := state.Load(cfg.QuadletDir); err != nil {
		notes = append(notes, "state unreadable: "+firstLine(err.Error()))
	} else {
		in.Recorded = st
	}

	if len(only) > 0 {
		if err := filterStatusInput(&in, only); err != nil {
			return in, notes, err
		}
	}

	in.Disk = map[string]string{}
	if existing, err := reconcile.ListExisting(cfg.QuadletDir); err == nil {
		owned := ownedPaths(in)
		for _, rel := range existing {
			// Under a filter, only the selected quadlets' files are in view;
			// unowned (foreign) files belong to no quadlet and drop out.
			if len(only) > 0 && !owned[rel] {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(cfg.QuadletDir, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			in.Disk[rel] = state.HashBytes(raw)
		}
	} else if !os.IsNotExist(err) {
		notes = append(notes, "quadlet dir unreadable: "+firstLine(err.Error()))
	}

	if services := serviceSet(in); len(services) > 0 {
		rt, err := systemd.Show(underHome(cfg.QuadletDir), services)
		if err != nil {
			notes = append(notes, "runtime unavailable: "+firstLine(err.Error()))
		} else {
			in.Runtime = rt
		}
	}
	return in, notes, nil
}

// filterStatusInput narrows desired and recorded state to the named quadlets,
// erroring on a name known to neither.
func filterStatusInput(in *statusInput, only []string) error {
	known := map[string]bool{}
	for _, q := range in.DesiredUnits {
		known[q.Name] = true
	}
	if in.Recorded != nil {
		for name := range in.Recorded.Quadlets {
			known[name] = true
		}
	}
	names := make([]string, 0, len(known))
	for n := range known {
		names = append(names, n)
	}
	sort.Strings(names)
	sel := map[string]bool{}
	for _, name := range only {
		if !known[name] {
			return fmt.Errorf("unknown quadlet %q (known: %s)", name, strings.Join(names, ", "))
		}
		sel[name] = true
	}

	var quads []eval.Quadlet
	for _, q := range in.DesiredUnits {
		if sel[q.Name] {
			quads = append(quads, q)
		}
	}
	in.DesiredUnits = quads
	if in.Desired != nil {
		files := map[string]map[string]state.FileInput{}
		for name, f := range in.Desired {
			if sel[name] {
				files[name] = f
			}
		}
		in.Desired = files
	}
	if in.Recorded != nil {
		filtered := &state.State{Version: in.Recorded.Version, CreiVersion: in.Recorded.CreiVersion, Quadlets: map[string]state.Quadlet{}}
		for name, q := range in.Recorded.Quadlets {
			if sel[name] {
				filtered.Quadlets[name] = q
			}
		}
		in.Recorded = filtered
	}
	return nil
}

// ownedPaths is every path attributable to a quadlet currently in view.
func ownedPaths(in statusInput) map[string]bool {
	owned := map[string]bool{}
	for _, files := range in.Desired {
		for p := range files {
			owned[p] = true
		}
	}
	if in.Recorded != nil {
		for p := range in.Recorded.FileOwner() {
			owned[p] = true
		}
	}
	return owned
}

// serviceSet is every systemd service name known for the units in view, from
// the eval manifest first, the recorded state as fallback.
func serviceSet(in statusInput) []string {
	seen := map[string]bool{}
	for _, q := range in.DesiredUnits {
		for _, u := range q.Units {
			if u.Service != "" {
				seen[u.Service] = true
			}
		}
	}
	if in.Recorded != nil {
		for _, q := range in.Recorded.Quadlets {
			for _, u := range q.Units {
				if u.Service != "" {
					seen[u.Service] = true
				}
			}
		}
	}
	services := make([]string, 0, len(seen))
	for s := range seen {
		services = append(services, s)
	}
	sort.Strings(services)
	return services
}

// unitMeta resolves a path's owning quadlet and service, desired manifest
// first, recorded state second.
func unitMeta(in statusInput, path string) (quadlet, service string, isUnit bool) {
	for _, q := range in.DesiredUnits {
		for _, u := range q.Units {
			if u.Filename == path {
				return q.Name, u.Service, true
			}
		}
	}
	if in.Recorded != nil {
		for name, q := range in.Recorded.Quadlets {
			for _, u := range q.Units {
				if u.Filename == path {
					return name, u.Service, true
				}
			}
		}
	}
	// Not a known unit file: an artifact (images/...) or a foreign file.
	if in.Desired != nil {
		for name, files := range in.Desired {
			if _, ok := files[path]; ok {
				return name, "", false
			}
		}
	}
	if owner, ok := in.Recorded.FileOwner()[path]; ok {
		return owner, "", false
	}
	return "", "", false
}

// classifyRows builds the table from the layer inputs. Pure: no IO.
func classifyRows(in statusInput) []statusRow {
	// Union of paths across desired, recorded, and disk.
	paths := map[string]bool{}
	desiredHash := map[string]string{}
	for _, files := range in.Desired {
		for p, f := range files {
			paths[p] = true
			desiredHash[p] = state.HashBytes(f.Content)
		}
	}
	if in.Recorded != nil {
		for _, q := range in.Recorded.Quadlets {
			for _, f := range q.Files {
				paths[f.Path] = true
			}
		}
	}
	for p := range in.Disk {
		paths[p] = true
	}

	var rows []statusRow
	for p := range paths {
		dHash, inDesired := desiredHash[p]
		rec, inState := in.Recorded.FileRecord(p)
		diskHash, onDisk := in.Disk[p]
		// A recorded file that is neither desired nor on disk has already
		// converged to "gone"; the next apply drops its record.
		if !inDesired && !onDisk {
			continue
		}
		if in.Desired == nil {
			inDesired = false // eval failed: judge by recorded state only
		}

		var disk string
		switch {
		case inDesired && onDisk && dHash == diskHash:
			disk = diskSynced
		case inDesired && onDisk:
			disk = diskPending
			if inState && rec.SHA256 != diskHash {
				disk = diskTampered
			}
		case inDesired:
			disk = diskMissing
		case in.Desired == nil && inState && onDisk:
			// No desired to compare: report disk relative to the last apply.
			disk = diskApplied
			if rec.SHA256 != diskHash {
				disk = diskTampered
			}
		case in.Desired == nil && inState:
			disk = diskMissing
		case onDisk && inState:
			disk = diskOrphan
		case onDisk && in.Recorded != nil:
			disk = diskForeign
		case onDisk && in.Desired == nil:
			disk = diskUnknown
		default: // onDisk, no state file, eval fine: today's whole-dir semantics
			disk = diskOrphan
		}

		quadlet, service, isUnit := unitMeta(in, p)
		row := statusRow{Quadlet: quadlet, Path: p, Service: service, Disk: disk, artifact: !isUnit}
		if service != "" && in.Runtime != nil {
			if rt, ok := in.Runtime[service]; ok {
				switch {
				case rt.LoadState == "not-found":
					row.Loaded = "not loaded"
				case rt.NeedDaemonReload:
					row.Loaded = "reload needed"
				case rt.LoadState != "":
					row.Loaded = "ok"
				}
				if rt.LoadState != "" && rt.LoadState != "not-found" {
					row.Runtime = rt.ActiveState
					if rt.Running() {
						row.Runtime = "running"
					}
					if !rt.ActiveEnter.IsZero() && rt.ActiveState == "active" {
						row.Since = in.Now.Sub(rt.ActiveEnter)
						if inState && rec.AppliedAt.After(rt.ActiveEnter) {
							row.Stale = true
						}
					}
				}
			}
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		qi, qj := rows[i].Quadlet, rows[j].Quadlet
		// Unowned rows sort last.
		if (qi == "") != (qj == "") {
			return qj == ""
		}
		if qi != qj {
			return qi < qj
		}
		if rows[i].artifact != rows[j].artifact {
			return rows[j].artifact
		}
		return rows[i].Path < rows[j].Path
	})
	return rows
}

// statusClean reports whether --check should pass: every layer readable, every
// file synced (or matching the last apply when eval is degraded), everything
// loaded fresh, nothing failed or stale.
func statusClean(rows []statusRow, notes []string) bool {
	for _, n := range notes {
		// A missing systemd is an environment, not a convergence, problem.
		if !strings.HasPrefix(n, "runtime unavailable") {
			return false
		}
	}
	for _, r := range rows {
		if r.Disk != diskSynced && r.Disk != diskApplied {
			return false
		}
		if r.Loaded == "reload needed" || r.Loaded == "not loaded" {
			return false
		}
		if r.Runtime == "failed" || r.Stale {
			return false
		}
	}
	return true
}

// --- rendering ---

// renderOpts tunes the table view. ShowAllArtifacts keeps synced images/ rows
// visible (drill-in and problems views); ProblemsOnly adjusts the empty-table
// message.
type renderOpts struct {
	ShowAllArtifacts bool
	ProblemsOnly     bool
}

func renderStatus(out io.Writer, quadletDir string, rows []statusRow, notes []string, in statusInput, opts renderOpts) {
	scope := "system"
	if underHome(quadletDir) {
		scope = "user"
	}
	fmt.Fprintf(out, "%s (%s scope)\n", quadletDir, scope)
	for _, n := range notes {
		fmt.Fprintln(out, yellow("! "+n))
	}
	fmt.Fprintln(out)
	if len(rows) == 0 {
		if opts.ProblemsOnly {
			fmt.Fprintln(out, green("No problems."))
		} else {
			fmt.Fprintln(out, "No units: nothing desired, recorded, or on disk.")
		}
		return
	}

	// The unfiltered overview hides synced images/ artifacts: they have no
	// LOADED/RUNTIME state, so a synced one is a row of pure noise. Any
	// non-synced artifact (pending/tampered/...) still shows, and naming a
	// quadlet shows everything.
	all := rows
	hiddenArtifacts := 0
	if !opts.ShowAllArtifacts {
		visible := rows[:0:0]
		for _, r := range rows {
			if r.artifact && r.Disk == diskSynced {
				hiddenArtifacts++
				continue
			}
			visible = append(visible, r)
		}
		rows = visible
	}

	// Rows are grouped under their quadlet name; column widths are computed
	// globally so every group aligns. The group header sits at column zero,
	// unit rows indent beneath it.
	const indent = "  "
	head := []string{"UNIT", "DISK", "LOADED", "RUNTIME"}
	cells := make([][]string, 0, len(rows)+1)
	cells = append(cells, head)
	for _, r := range rows {
		cells = append(cells, []string{r.Path, r.Disk, dash(r.Loaded), runtimeCell(r)})
	}
	widths := make([]int, len(head))
	for _, row := range cells {
		for i, c := range row {
			if len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	line := func(row []string, header bool) string {
		var b strings.Builder
		for i, c := range row {
			pad := strings.Repeat(" ", widths[i]-len(c)+2)
			if header {
				b.WriteString(dim(c))
			} else {
				b.WriteString(styleCell(i, row, c))
			}
			if i < len(row)-1 {
				b.WriteString(pad)
			}
		}
		return strings.TrimRight(b.String(), " ")
	}
	fmt.Fprintln(out, indent+line(head, true))
	group := "\x00" // sentinel: no group printed yet
	for ri, r := range rows {
		if r.Quadlet != group {
			if group != "\x00" {
				fmt.Fprintln(out)
			}
			group = r.Quadlet
			name := group
			if name == "" {
				name = "(unmanaged)"
			}
			fmt.Fprintln(out, bold(name))
		}
		fmt.Fprintln(out, glyphFor(r)+" "+line(cells[ri+1], false))
	}

	fmt.Fprintln(out)
	disk := diskSummary(all)
	if hiddenArtifacts > 0 {
		disk += dim(fmt.Sprintf(" · %d synced images/ hidden", hiddenArtifacts))
	}
	fmt.Fprintln(out, disk)
	if rt := runtimeSummary(all); rt != "" {
		fmt.Fprintln(out, rt)
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runtimeCell(r statusRow) string {
	if r.Runtime == "" {
		return "-"
	}
	cell := r.Runtime
	if r.Since > 0 {
		cell += " " + humanDuration(r.Since)
	}
	if r.Stale {
		cell += " (stale)"
	}
	return cell
}

// glyphFor is the systemctl-style state dot at the start of each unit row:
// the fastest health read on a stack is one column of green dots. Healthy
// covers active as well as running (oneshot network/volume/build units are
// "active (exited)" forever).
func glyphFor(r statusRow) string {
	switch {
	case r.Runtime == "":
		return " "
	case r.Runtime == "failed":
		return red("✗")
	case r.Stale:
		return yellow("●")
	case r.Runtime == "running" || r.Runtime == "active":
		return green("●")
	case r.Runtime == "inactive":
		return dim("○")
	default: // activating, deactivating, reloading, ...
		return "◌"
	}
}

// styleCell colors DISK and RUNTIME cells; the padding math runs on the plain
// string, so ANSI codes never skew column widths.
func styleCell(col int, row []string, c string) string {
	switch col {
	case 1: // DISK
		switch c {
		case diskSynced, diskApplied:
			return green(c)
		case diskTampered:
			return red(c)
		case diskForeign, diskUnknown:
			return dim(c)
		default:
			return yellow(c)
		}
	case 2: // LOADED
		if c == "reload needed" || c == "not loaded" {
			return yellow(c)
		}
		return c
	case 3: // RUNTIME
		switch {
		case strings.HasPrefix(c, "failed"):
			return red(c)
		case strings.Contains(c, "(stale)"):
			return yellow(c)
		// "activating" does not prefix-match "active": the words diverge
		// at the sixth character.
		case strings.HasPrefix(c, "running"), strings.HasPrefix(c, "active"):
			return green(c)
		case c == "-":
			return c
		default:
			return dim(c)
		}
	}
	return c
}

// diskSummary is the footer's first line: file totals by disk state.
func diskSummary(rows []statusRow) string {
	disk := map[string]int{}
	for _, r := range rows {
		disk[r.Disk]++
	}
	var parts []string
	for _, k := range []string{diskSynced, diskApplied, diskPending, diskTampered, diskMissing, diskOrphan, diskForeign, diskUnknown} {
		if disk[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", disk[k], k))
		}
	}
	return fmt.Sprintf("%d file(s): %s", len(rows), strings.Join(parts, ", "))
}

// runtimeSummary is the footer's second line, empty when no runtime state is
// known. Failure counts are highlighted so the eye lands on them.
func runtimeSummary(rows []statusRow) string {
	run := map[string]int{}
	for _, r := range rows {
		if r.Runtime != "" {
			key := r.Runtime
			if r.Stale {
				key += " stale"
			}
			run[key]++
		}
	}
	if len(run) == 0 {
		return ""
	}
	var parts []string
	for _, k := range []string{"running", "running stale", "active", "active stale", "failed", "inactive", "activating"} {
		n := run[k]
		delete(run, k)
		if n == 0 {
			continue
		}
		p := fmt.Sprintf("%d %s", n, k)
		switch k {
		case "failed":
			p = red(p)
		case "running stale", "active stale":
			p = yellow(p)
		}
		parts = append(parts, p)
	}
	// Any states outside the common set (deactivating, reloading, ...) still
	// count rather than silently vanishing.
	rest := make([]string, 0, len(run))
	for k := range run {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", run[k], k))
	}
	return "runtime: " + strings.Join(parts, ", ")
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= 72*time.Hour: // past a few days, day granularity is enough
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	case d >= 24*time.Hour:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, hours)
	case d >= time.Hour: // past an hour, minute granularity is noise
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// statusJSON is the --format json document: every row (nothing hidden), plus
// the degradation notes and the overall clean verdict, so scripts need no
// parsing of table text.
type statusJSON struct {
	QuadletDir string          `json:"quadletDir"`
	Scope      string          `json:"scope"`
	Notes      []string        `json:"notes"`
	Clean      bool            `json:"clean"`
	Rows       []statusRowJSON `json:"rows"`
}

type statusRowJSON struct {
	Quadlet      string `json:"quadlet"`
	Path         string `json:"path"`
	Service      string `json:"service,omitempty"`
	Artifact     bool   `json:"artifact"`
	Disk         string `json:"disk"`
	Loaded       string `json:"loaded,omitempty"`
	Runtime      string `json:"runtime,omitempty"`
	SinceSeconds int64  `json:"sinceSeconds,omitempty"`
	Stale        bool   `json:"stale"`
}

func renderStatusJSON(out io.Writer, quadletDir string, rows []statusRow, notes []string) error {
	scope := "system"
	if underHome(quadletDir) {
		scope = "user"
	}
	doc := statusJSON{
		QuadletDir: quadletDir,
		Scope:      scope,
		Notes:      append([]string{}, notes...),
		Clean:      statusClean(rows, notes),
		Rows:       make([]statusRowJSON, 0, len(rows)),
	}
	for _, r := range rows {
		doc.Rows = append(doc.Rows, statusRowJSON{
			Quadlet:      r.Quadlet,
			Path:         r.Path,
			Service:      r.Service,
			Artifact:     r.artifact,
			Disk:         r.Disk,
			Loaded:       r.Loaded,
			Runtime:      r.Runtime,
			SinceSeconds: int64(r.Since / time.Second),
			Stale:        r.Stale,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// errHead condenses a multi-line error to its informative head: cue errors
// start with a "build <dir>:" line whose actual message is on the next line,
// so a first line ending in ':' pulls the following line up.
func errHead(err error) string {
	lines := strings.Split(err.Error(), "\n")
	head := strings.TrimSpace(lines[0])
	if strings.HasSuffix(head, ":") && len(lines) > 1 {
		head += " " + strings.TrimSpace(lines[1])
	}
	return head
}
